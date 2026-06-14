// H1 — endpoints de SSO/OIDC. Só ativos quando s.cfg.OIDC != nil
// (-auth-mode=oidc configurado). O login local segue intacto em paralelo.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/Dr0nj/regente-server/internal/auth"
)

const oidcStateCookie = "regente_oidc_state"

// oidcLogin — GET /api/auth/oidc/login. Gera state, guarda em cookie e
// redireciona para o IdP.
func (s *server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.OIDC == nil {
		http.Error(w, "oidc not configured", http.StatusNotFound)
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "state", http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.cfg.OIDC.AuthCodeURL(state), http.StatusFound)
}

// oidcCallback — GET /api/auth/oidc/callback. Valida o state, troca o code,
// provisiona/abre sessão e redireciona pro app com o token no fragment.
func (s *server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if s.cfg.OIDC == nil {
		http.Error(w, "oidc not configured", http.StatusNotFound)
		return
	}
	state := r.URL.Query().Get("state")
	ck, err := r.Cookie(oidcStateCookie)
	if err != nil || state == "" || ck.Value != state {
		http.Error(w, "invalid oidc state", http.StatusBadRequest)
		return
	}
	// limpa o cookie de state
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/", MaxAge: -1})

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	identity, err := s.cfg.OIDC.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "oidc exchange: "+err.Error(), http.StatusBadGateway)
		return
	}
	role := auth.Role(s.cfg.OIDC.Config().DefaultRole)
	token, _, err := auth.LoginFederated(s.cfg.DB, identity.Username(), role)
	if err != nil {
		http.Error(w, "federated login: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Redireciona pro app com o token no fragment (#token=...), que não é
	// enviado a servidores. O SPA lê o hash e chama setAuthToken. (Hardening
	// futuro: cookie HttpOnly + sessão por cookie — ver README H1.)
	dest := s.cfg.AppURL
	if dest == "" {
		dest = "/"
	}
	http.Redirect(w, r, dest+"#token="+token, http.StatusFound)
}
