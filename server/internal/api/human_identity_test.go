package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/gorilla/websocket"
)

func TestHumanIdentity(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			admin, err := auth.CreateUser(d, "admin", "strong-test-password", auth.RoleAdmin)
			if err != nil {
				t.Fatal(err)
			}
			if err = auth.ChangePassword(d, admin.ID, "", "strong-test-password", true); err != nil {
				t.Fatal(err)
			}
			t.Run("issuer_subject_not_name_email", func(t *testing.T) {
				a, ua, e := auth.LoginExternal(d, "https://issuer-a", "same-sub", "admin", auth.RoleViewer, time.Now().Add(time.Hour))
				if e != nil {
					t.Fatal(e)
				}
				_, ub, e := auth.LoginExternal(d, "https://issuer-b", "same-sub", "admin", auth.RoleViewer, time.Now().Add(time.Hour))
				if e != nil {
					t.Fatal(e)
				}
				if ua.ID == admin.ID || ua.ID == ub.ID || ua.Role != auth.RoleViewer {
					t.Fatal("colisão elevou/vinculou identidade")
				}
				_, again, e := auth.LoginExternal(d, "https://issuer-a", "same-sub", "renamed", auth.RoleAdmin, time.Now().Add(time.Hour))
				if e != nil || again.ID != ua.ID || again.Role != auth.RoleViewer {
					t.Fatal("relogin mudou identidade/papel")
				}
				var raw string
				var expires time.Time
				if e = d.QueryRow("SELECT token,expires_at FROM sessions WHERE token=?", auth.Digest(a)).Scan(&raw, &expires); e != nil {
					t.Fatal(e)
				}
				if raw == a || expires.After(time.Now().Add(5*time.Minute)) {
					t.Fatal("segredo persistido ou expiração sem limite")
				}
				if e = auth.SetAccess(d, "admin", ua.ID, true); e != nil {
					t.Fatal(e)
				}
				if _, e = auth.Resolve(d, a); e == nil {
					t.Fatal("usuário bloqueado manteve sessão")
				}
				if _, _, e = auth.LoginExternal(d, "https://issuer-a", "same-sub", "admin", auth.RoleAdmin, time.Now().Add(time.Hour)); e == nil {
					t.Fatal("bloqueio contornado por SSO")
				}
			})
			t.Run("explicit_link_and_audit", func(t *testing.T) {
				if e := auth.LinkExternal(d, "admin", admin.ID, "https://issuer-link", "verified-sub"); e != nil {
					t.Fatal(e)
				}
				_, u, e := auth.LoginExternal(d, "https://issuer-link", "verified-sub", "any", auth.RoleViewer, time.Now().Add(time.Hour))
				if e != nil || u.ID != admin.ID {
					t.Fatal("link explícito não aplicado")
				}
				if e = auth.LinkExternal(d, "admin", admin.ID, "https://issuer-link", "verified-sub"); e == nil {
					t.Fatal("remapeamento aceito")
				}
				var n int
				if e = d.QueryRow("SELECT COUNT(*) FROM identity_audit WHERE action='link'").Scan(&n); e != nil || n != 1 {
					t.Fatal("auditoria transacional ausente")
				}
			})
			for _, mode := range []string{"local", "hybrid", "oidc", "invalid"} {
				t.Run(mode, func(t *testing.T) {
					router := NewRouter(Config{DB: d, Hub: hub.New(), AuthMode: mode, Token: "legacy", EmergencyUser: "admin"})
					call := func(method, path, body, token, csrf string, cookie *http.Cookie) *httptest.ResponseRecorder {
						r := httptest.NewRequest(method, path, strings.NewReader(body))
						r.Header.Set("Content-Type", "application/json")
						if token != "" {
							r.Header.Set("Authorization", "Bearer "+token)
						}
						if cookie != nil {
							r.AddCookie(cookie)
						}
						r.Header.Set("X-CSRF-Token", csrf)
						w := httptest.NewRecorder()
						router.ServeHTTP(w, r)
						return w
					}
					w := call("POST", "/api/auth/login", `{"username":"admin","password":"strong-test-password","browser":true}`, "", "", nil)
					if mode == "oidc" || mode == "invalid" {
						if w.Code != 403 {
							t.Fatalf("SSO obrigatório aceitou senha: %d", w.Code)
						}
						if call("GET", "/api/auth/me", "", "legacy", "", nil).Code != 401 {
							t.Fatal("bypass administrativo aceito")
						}
						return
					}
					if w.Code != 200 {
						t.Fatalf("senha local/híbrida recusada: %d %s", w.Code, w.Body.String())
					}
					var body map[string]any
					if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["token"] != "" {
						t.Fatal("sessão do navegador exposta em JSON")
					}
					cookies := w.Result().Cookies()
					if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
						t.Fatal("cookie desprotegido")
					}
					ck := cookies[0]
					csrf := w.Header().Get("X-CSRF-Token")
					if call("POST", "/api/auth/logout", "", "", "", ck).Code != 403 {
						t.Fatal("CSRF ausente aceito")
					}
					if call("GET", "/api/auth/me", "", ck.Value, "", nil).Code != 403 {
						t.Fatal("cookie convertido em bearer")
					}
					if call("GET", "/api/auth/me?token="+ck.Value, "", "", "", nil).Code != 401 {
						t.Fatal("token na URL aceito")
					}
					if call("GET", "/api/auth/me", "", "", "", ck).Code != 200 {
						t.Fatal("sessão cookie inválida")
					}
					if call("POST", "/api/auth/logout", "", "", csrf, ck).Code != 204 {
						t.Fatal("logout falhou")
					}
					if call("GET", "/api/auth/me", "", "", "", ck).Code != 401 {
						t.Fatal("logout não revogou")
					}
					// CLI mantém bearer explícito e não depende de cookies/CSRF.
					w = call("POST", "/api/auth/login", `{"username":"admin","password":"strong-test-password"}`, "", "", nil)
					var cli struct{ Token string }
					_ = json.Unmarshal(w.Body.Bytes(), &cli)
					if cli.Token == "" || call("GET", "/api/auth/me", "", cli.Token, "", nil).Code != 200 {
						t.Fatal("login CLI quebrou")
					}
				})
			}
			t.Run("emergency_and_origins", func(t *testing.T) {
				router := NewRouter(Config{DB: d, Hub: hub.New(), AuthMode: "oidc", EmergencyUser: "admin", AppURL: "https://regente.example"})
				r := httptest.NewRequest("POST", "https://regente.example/api/auth/login", strings.NewReader(`{"username":"admin","password":"strong-test-password","emergency":true,"browser":true}`))
				r.Header.Set("Origin", "https://regente.example")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if w.Code != 200 || !w.Result().Cookies()[0].Secure || w.Result().Cookies()[0].MaxAge != 900 {
					t.Fatalf("emergência não segregada: %d", w.Code)
				}
				r = httptest.NewRequest("POST", "https://regente.example/api/auth/login", strings.NewReader(`{}`))
				r.Header.Set("Origin", "https://evil.example")
				w = httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if w.Code != 403 {
					t.Fatal("login CSRF aceito")
				}
			})
			t.Run("event_ticket_two_nodes_replay_and_revoke", func(t *testing.T) {
				token, _, e := auth.Login(d, "admin", "strong-test-password")
				if e != nil {
					t.Fatal(e)
				}
				a := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), AuthMode: "hybrid"}))
				defer a.Close()
				b := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), AuthMode: "hybrid"}))
				defer b.Close()
				raw := machineRequest(t, a, "POST", "/api/auth/event-ticket", token, nil, 200)
				var issued struct{ Ticket string }
				if json.Unmarshal(raw, &issued) != nil {
					t.Fatal("ticket inválido")
				}
				url := "ws" + strings.TrimPrefix(b.URL, "http") + "/ws/web?ticket=" + issued.Ticket
				conn, _, e := websocket.DefaultDialer.Dial(url, nil)
				if e != nil {
					t.Fatal(e)
				}
				defer conn.Close()
				if replay, _, e := websocket.DefaultDialer.Dial(url, nil); e == nil {
					replay.Close()
					t.Fatal("ticket reutilizado")
				}
				if e = auth.Logout(d, token); e != nil {
					t.Fatal(e)
				}
				_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				if _, _, e = conn.ReadMessage(); e == nil {
					t.Fatal("sessão revogada permaneceu aberta")
				}
				if websocket.IsUnexpectedCloseError(e, websocket.CloseAbnormalClosure) {
					t.Logf("socket encerrado: %v", e)
				}
				if n, ok := e.(interface{ Timeout() bool }); ok && n.Timeout() {
					t.Fatal("revogação só terminou pelo deadline")
				}
			})
		})
	}
}
