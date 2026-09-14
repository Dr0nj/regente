package api

import (
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/Dr0nj/regente-server/internal/audit"
	"github.com/Dr0nj/regente-server/internal/auth"
	"golang.org/x/oauth2"
)

const oidcStateCookie = "regente_oidc_state"

func (s *server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.mode() == "local" || s.cfg.OIDC == nil {
		http.Error(w, "SSO is unavailable", http.StatusServiceUnavailable)
		return
	}
	state, nonce, verifier := oauth2.GenerateVerifier(), oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	_, _ = s.cfg.DB.Exec("DELETE FROM auth_transactions WHERE expires_at<?", time.Now())
	_, err := s.cfg.DB.Exec("INSERT INTO auth_transactions(state_hash,nonce,verifier,expires_at) VALUES(?,?,?,?)", auth.Digest(state), nonce, verifier, time.Now().Add(5*time.Minute))
	if err != nil {
		http.Error(w, "Unable to start SSO", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(r, oidcStateCookie), Value: state, Path: "/", MaxAge: 300, HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, s.cfg.OIDC.AuthCodeURL(state, nonce, verifier), http.StatusFound)
}

func (s *server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if s.mode() == "local" || s.cfg.OIDC == nil {
		http.Error(w, "SSO is unavailable", http.StatusServiceUnavailable)
		return
	}
	state := r.URL.Query().Get("state")
	ck, err := r.Cookie(s.cookieName(r, oidcStateCookie))
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(ck.Value), []byte(state)) != 1 {
		http.Error(w, "Invalid SSO state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(r, oidcStateCookie), Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteLaxMode})
	var nonce, verifier string
	// DELETE RETURNING consome uma única vez mesmo em callback concorrente em outro nó.
	err = s.cfg.DB.QueryRow("DELETE FROM auth_transactions WHERE state_hash=? AND expires_at>? RETURNING nonce,verifier", auth.Digest(state), time.Now()).Scan(&nonce, &verifier)
	if err != nil || r.URL.Query().Get("code") == "" {
		http.Error(w, "Expired or already used SSO login", http.StatusBadRequest)
		return
	}
	identity, err := s.cfg.OIDC.Exchange(r.Context(), r.URL.Query().Get("code"), nonce, verifier)
	if err != nil {
		s.audit(audit.Event{Type: "auth.oidc", Action: "login", Outcome: "failure", IP: clientIP(r)})
		http.Error(w, "SSO verification failed", http.StatusUnauthorized)
		return
	}
	token, u, err := auth.LoginExternal(s.cfg.DB, identity.Issuer, identity.Subject, identity.Username(), auth.Role(s.cfg.OIDC.Config().DefaultRole), identity.ExpiresAt)
	if err != nil {
		http.Error(w, "SSO account is unavailable; contact an administrator", http.StatusForbidden)
		return
	}
	s.setSessionCookie(w, r, token, 300)
	s.audit(audit.Event{Type: "auth.oidc", Actor: u.Username, Action: "login", Outcome: "success", IP: clientIP(r)})
	dest := s.cfg.AppURL
	if dest == "" {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}
