package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/oidc"
)

func TestHumanIdentityOIDCState(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			var base string
			var exchanges atomic.Int32
			idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					exchanges.Add(1)
					http.Error(w, "synthetic refusal", 400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/keys"})
			}))
			defer idp.Close()
			base = idp.URL
			p, err := oidc.Discover(context.Background(), oidc.Config{Issuer: base, ClientID: "client", RedirectURL: "http://127.0.0.1/callback"})
			if err != nil {
				t.Fatal(err)
			}
			a, b := NewRouter(Config{DB: d, OIDC: p, AuthMode: "hybrid"}), NewRouter(Config{DB: d, OIDC: p, AuthMode: "hybrid"})
			start := httptest.NewRecorder()
			a.ServeHTTP(start, httptest.NewRequest("GET", "/api/auth/oidc/login", nil))
			location, _ := url.Parse(start.Header().Get("Location"))
			state := location.Query().Get("state")
			if state == "" {
				t.Fatal("state ausente")
			}
			ck := start.Result().Cookies()[0]
			callback := func(cookie *http.Cookie) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/api/auth/oidc/callback?state="+state+"&code=code", nil)
				if cookie != nil {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				b.ServeHTTP(w, r)
				return w
			}
			if callback(nil).Code != 400 || callback(&http.Cookie{Name: oidcStateCookie, Value: "wrong"}).Code != 400 {
				t.Fatal("cookie ausente/errado aceito")
			}
			if exchanges.Load() != 0 {
				t.Fatal("state inválido chegou à troca")
			}
			if callback(ck).Code != 401 {
				t.Fatal("troca negativa não recusada")
			}
			count := exchanges.Load()
			if count == 0 {
				t.Fatal("callback válido não consumiu transação")
			}
			if callback(ck).Code != 400 || exchanges.Load() != count {
				t.Fatal("replay chegou ao IdP")
			}
			// Expiração é aplicada no banco compartilhado, sem depender do cookie do cliente.
			_, err = d.Exec("INSERT INTO auth_transactions(state_hash,nonce,verifier,expires_at) VALUES(?,?,?,?)", auth.Digest(state), "nonce", "verifier", time.Now().Add(-time.Second))
			if err != nil {
				t.Fatal(err)
			}
			if callback(ck).Code != 400 {
				t.Fatal("transação expirada aceita")
			}
			if strings.Contains(start.Header().Get("Location"), "code_verifier") {
				t.Fatal("verifier vazou na URL")
			}
		})
	}
}
