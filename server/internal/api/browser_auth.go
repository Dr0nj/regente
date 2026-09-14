package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
)

func (s *server) mode() string {
	// Config direta usada por integrações antigas com Provider explícito.
	if s.cfg.AuthMode == "" && s.cfg.OIDC != nil {
		return "hybrid"
	}
	m, err := auth.Mode(s.cfg.AuthMode)
	if err != nil {
		return "invalid"
	}
	return m
}

func (s *server) secureCookie(r *http.Request) bool {
	trusted := s.cfg.TrustedProxies
	if len(trusted) == 0 {
		trusted = trustedProxiesDefault
	}
	if peer, err := addrFromHostPort(r.RemoteAddr); err == nil && isTrusted(peer, trusted) && r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return r.TLS != nil || len(s.cfg.AppURL) >= 8 && s.cfg.AppURL[:8] == "https://" || s.cfg.OIDC != nil && len(s.cfg.OIDC.Config().RedirectURL) >= 8 && s.cfg.OIDC.Config().RedirectURL[:8] == "https://"
}

func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(r, "regente_session"), Value: token, Path: "/", HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (s *server) cookieName(r *http.Request, name string) string {
	if s.secureCookie(r) {
		return "__Host-" + name
	}
	return name
}

func (s *server) allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return false
	}
	scheme := "http"
	if s.secureCookie(r) {
		scheme = "https"
	}
	if origin == scheme+"://"+r.Host {
		return true
	}
	app, err := url.Parse(s.cfg.AppURL)
	return err == nil && app.Host != "" && origin == app.Scheme+"://"+app.Host
}

func (s *server) authConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	m := s.mode()
	writeJSON(w, http.StatusOK, map[string]any{"mode": m, "local": m == "local" || m == "hybrid", "sso": m == "oidc" || m == "hybrid", "ssoAvailable": s.cfg.OIDC != nil, "emergency": s.cfg.EmergencyUser != ""})
}

func (s *server) sessionAllowed(u *auth.User) bool {
	m := s.mode()
	if m == "invalid" {
		return false
	}
	if u.Source == "emergency" {
		return s.cfg.EmergencyUser != "" && u.Username == s.cfg.EmergencyUser && u.Role == auth.RoleAdmin
	}
	if u.Source == "oidc" {
		return m == "hybrid" || m == "oidc"
	}
	return m == "local" || m == "hybrid"
}

func (s *server) browserCSRF(w http.ResponseWriter, r *http.Request, u *auth.User) bool {
	if r.Header.Get("Authorization") != "" {
		return !u.Browser
	}
	if !u.Browser {
		return false
	}
	if s.secureCookie(r) {
		if _, err := r.Cookie("__Host-regente_session"); err != nil {
			return false
		}
	}
	if !s.allowedOrigin(r) {
		return false
	}
	w.Header().Set("X-CSRF-Token", u.CSRF)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	return u.CSRF != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(u.CSRF)) == 1
}

func (s *server) authEventTicket(w http.ResponseWriter, r *http.Request) {
	_, _ = s.cfg.DB.Exec("DELETE FROM web_tickets WHERE expires_at<?", time.Now())
	ticket := oauth2.GenerateVerifier()
	_, err := s.cfg.DB.Exec("INSERT INTO web_tickets(token_hash,session_token,expires_at) VALUES(?,?,?)", auth.Digest(ticket), auth.Digest(auth.ExtractToken(r)), time.Now().Add(30*time.Second))
	if err != nil {
		http.Error(w, "Unable to authorize events", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket})
}

func (s *server) linkIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid user", 400)
		return
	}
	var body struct {
		Issuer  string `json:"issuer"`
		Subject string `json:"subject"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "Invalid identity", 400)
		return
	}
	u, _ := auth.FromContext(r.Context())
	var name string
	if e := s.cfg.DB.QueryRow("SELECT username FROM users WHERE id=?", id).Scan(&name); e != nil || name == s.cfg.EmergencyUser {
		http.Error(w, "Emergency accounts cannot be linked to SSO", http.StatusBadRequest)
		return
	}
	parsed, e := url.Parse(body.Issuer)
	if e != nil || parsed.Host == "" || (!strings.HasPrefix(body.Issuer, "https://") && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1") {
		http.Error(w, "Invalid issuer URL", http.StatusBadRequest)
		return
	}
	if err = auth.LinkExternal(s.cfg.DB, u.Username, id, body.Issuer, body.Subject); err != nil {
		http.Error(w, "Identity cannot be linked; verify the account and issuer/subject", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) userAccess(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid user", 400)
		return
	}
	var body struct {
		Disabled bool `json:"disabled"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "Invalid access policy", 400)
		return
	}
	u, _ := auth.FromContext(r.Context())
	if u.ID == id {
		http.Error(w, "Cannot disable your own access", 400)
		return
	}
	if err = auth.SetAccess(s.cfg.DB, u.Username, id, body.Disabled); err != nil {
		http.Error(w, "Unable to update access", 400)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
