package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/gorilla/websocket"
)

func machineTestDB(t *testing.T, dialect db.Dialect) *db.DB {
	t.Helper()
	if dialect == db.SQLite {
		d := newTestDB(t)
		t.Cleanup(func() { d.Close() })
		return d
	}
	dsn := os.Getenv("REGENTE_TEST_PG_DSN")
	if dsn == "" {
		if os.Getenv("REGENTE_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("Postgres obrigatório")
		}
		t.Skip("Postgres real não configurado")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("DSN deve ser URL PostgreSQL")
	}
	admin, err := db.Open(db.Postgres, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "regente_identity_" + randID()
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	d, err := db.Open(db.Postgres, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err = db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	return d
}

type issuedMachine struct {
	ID    int64  `json:"id"`
	Token string `json:"token"`
}

func machineRequest(t *testing.T, srv *httptest.Server, method, path, token string, body any, want int) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status=%d esperado=%d", method, path, resp.StatusCode, want)
	}
	return out
}

func issueTestMachine(t *testing.T, srv *httptest.Server, id string) issuedMachine {
	t.Helper()
	body := tokenRequest{AgentID: id, Environment: "", Capabilities: []string{"COMMAND"}, ExpiresAt: time.Now().Add(time.Hour)}
	raw := machineRequest(t, srv, "POST", "/api/agents/tokens", "test-token", body, 200)
	var c issuedMachine
	if json.Unmarshal(raw, &c) != nil || c.ID == 0 || len(c.Token) != 69 {
		t.Fatal("emissão inválida")
	}
	return c
}

func TestMachineIdentity(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			hubs := []*hub.Hub{hub.New(), hub.New()}
			servers := []*httptest.Server{}
			for _, h := range hubs {
				store := storage.NewFileStore(t.TempDir(), false)
				sched := scheduler.New(store, d, h, time.Hour)
				t.Cleanup(sched.Stop)
				srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Scheduler: sched, Store: store, Token: "test-token"}))
				t.Cleanup(srv.Close)
				servers = append(servers, srv)
			}
			a := issueTestMachine(t, servers[0], "worker-a")
			b := issueTestMachine(t, servers[0], "worker-b")
			t.Run("hash_and_immutable_scope", func(t *testing.T) {
				var hash string
				if err := d.QueryRow(`SELECT token_hash FROM agent_tokens WHERE id=?`, a.ID).Scan(&hash); err != nil {
					t.Fatal(err)
				}
				if hash != tokenDigest(a.Token) || strings.Contains(hash, a.Token) {
					t.Fatal("segredo não protegido no banco")
				}
				list := machineRequest(t, servers[0], "GET", "/api/agents/tokens", "test-token", nil, 200)
				if strings.Contains(string(list), a.Token) || strings.Contains(string(list), hash) {
					t.Fatal("listagem expôs segredo/hash")
				}
				body := tokenRequest{AgentID: "worker-a", Environment: "prod", Capabilities: []string{"COMMAND"}, ExpiresAt: time.Now().Add(time.Hour)}
				machineRequest(t, servers[0], "POST", "/api/agents/tokens", "test-token", body, 409)
				body.AgentID = "SERVER-AGENT"
				machineRequest(t, servers[0], "POST", "/api/agents/tokens", "test-token", body, 400)
				body.AgentID = "worker-new"
				body.ExpiresAt = time.Now().Add(-time.Hour)
				machineRequest(t, servers[0], "POST", "/api/agents/tokens", "test-token", body, 400)
			})
			t.Run("claims_rejected_on_all_transports", func(t *testing.T) {
				for _, path := range []string{"/ws/agent", "/api/agent/poll", "/api/agent/events"} {
					for _, query := range []string{"id=worker-b&caps=COMMAND", "id=worker-a&caps=COMMAND&env=prod", "id=worker-a&caps=SCRIPT", "caps=COMMAND"} {
						machineRequest(t, servers[0], "GET", path+"?"+query, a.Token, nil, 403)
					}
					machineRequest(t, servers[0], "GET", path+"?id=worker-a&caps=COMMAND&token="+a.Token, "", nil, 401)
				}
			})
			t.Run("assignment_http_and_ws", func(t *testing.T) {
				seedInstance(t, d, "assigned-b", "ops", "RUNNING")
				if _, err := d.Exec(`UPDATE instances SET agent_id='worker-b' WHERE id='assigned-b'`); err != nil {
					t.Fatal(err)
				}
				for _, event := range []string{"output", "result"} {
					body := map[string]any{"event": event, "instanceId": "assigned-b", "chunk": "forged", "output": "forged", "exitCode": 0}
					machineRequest(t, servers[1], "POST", "/api/agent/"+event, a.Token, body, 403)
					wsURL := "ws" + strings.TrimPrefix(servers[0].URL, "http") + "/ws/agent?id=worker-a&caps=COMMAND"
					conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": []string{"Bearer " + a.Token}})
					if err != nil {
						t.Fatal(err)
					}
					if err = conn.WriteJSON(body); err != nil {
						t.Fatal(err)
					}
					_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
					if _, _, err = conn.ReadMessage(); err == nil {
						t.Fatal("WS aceitou mensagem para outro agente")
					}
					conn.Close()
				}
				var n int
				if err := d.QueryRow(`SELECT COUNT(*) FROM instance_output WHERE instance_id='assigned-b'`).Scan(&n); err != nil || n != 0 {
					t.Fatal("output forjado persistido", err)
				}
				machineRequest(t, servers[1], "POST", "/api/agent/output", b.Token, map[string]any{"instanceId": "assigned-b", "chunk": "real"}, 200)
				machineRequest(t, servers[1], "POST", "/api/agent/result", b.Token, map[string]any{"instanceId": "assigned-b", "exitCode": 0}, 200)
				machineRequest(t, servers[1], "POST", "/api/agent/output", b.Token, map[string]any{"instanceId": "assigned-b", "chunk": "late"}, 403)
			})
			t.Run("rotation_overlap_and_single_use", func(t *testing.T) {
				old := issueTestMachine(t, servers[0], "rotation-overlap")
				path := fmt.Sprintf("/api/agents/tokens/%d/rotate", old.ID)
				body := map[string]any{"expiresAt": time.Now().Add(time.Hour), "graceSeconds": 1}
				raw := machineRequest(t, servers[0], "POST", path, "test-token", body, 200)
				var replacement issuedMachine
				if json.Unmarshal(raw, &replacement) != nil {
					t.Fatal("rotação sem credencial")
				}
				machineRequest(t, servers[1], "POST", path, "test-token", body, 404)
				for _, token := range []string{old.Token, replacement.Token} {
					machineRequest(t, servers[1], "POST", "/api/agent/output", token, map[string]string{"instanceId": "missing"}, 403)
				}
				time.Sleep(1100 * time.Millisecond)
				machineRequest(t, servers[1], "POST", "/api/agent/output", old.Token, map[string]string{"instanceId": "missing"}, 401)
				machineRequest(t, servers[1], "POST", "/api/agent/output", replacement.Token, map[string]string{"instanceId": "missing"}, 403)
			})
			for _, transport := range []string{"ws", "poll", "events"} {
				for _, action := range []string{"revoke", "expire", "rotate"} {
					t.Run(transport+"_"+action+"_two_nodes", func(t *testing.T) {
						id := "lease-" + transport + "-" + action
						c := issueTestMachine(t, servers[0], id)
						closed := make(chan error, 2)
						for _, srv := range servers {
							if transport == "ws" {
								conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/agent?id="+id+"&caps=COMMAND", http.Header{"Authorization": []string{"Bearer " + c.Token}})
								if err != nil {
									t.Fatal(err)
								}
								defer conn.Close()
								go func() { _, _, err := conn.ReadMessage(); closed <- err }()
							} else {
								ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
								defer cancel()
								req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/agent/"+transport+"?id="+id+"&caps=COMMAND", nil)
								req.Header.Set("Authorization", "Bearer "+c.Token)
								go func() {
									resp, err := srv.Client().Do(req)
									if err == nil {
										_, err = io.Copy(io.Discard, resp.Body)
										resp.Body.Close()
									}
									closed <- err
								}()
							}
						}
						deadline := time.Now().Add(3 * time.Second)
						for (!hubs[0].IsOnline(id) || !hubs[1].IsOnline(id)) && time.Now().Before(deadline) {
							time.Sleep(time.Millisecond)
						}
						if !hubs[0].IsOnline(id) || !hubs[1].IsOnline(id) {
							t.Fatal("conexão não registrada")
						}
						started := time.Now()
						switch action {
						case "revoke":
							machineRequest(t, servers[0], "DELETE", fmt.Sprintf("/api/agents/tokens/%d", c.ID), "test-token", nil, 200)
						case "expire":
							if _, err := d.Exec(`UPDATE agent_tokens SET expires_at=? WHERE id=?`, time.Now().Add(100*time.Millisecond).UnixMilli(), c.ID); err != nil {
								t.Fatal(err)
							}
						case "rotate":
							raw := machineRequest(t, servers[0], "POST", fmt.Sprintf("/api/agents/tokens/%d/rotate", c.ID), "test-token", map[string]any{"expiresAt": time.Now().Add(time.Hour), "graceSeconds": 0}, 200)
							var replacement issuedMachine
							if json.Unmarshal(raw, &replacement) != nil || replacement.Token == c.Token || replacement.Token == "" {
								t.Fatal("rotação inválida")
							}
							machineRequest(t, servers[1], "POST", "/api/agent/output", replacement.Token, map[string]any{"instanceId": "missing"}, 403)
						}
						for i := 0; i < 2; i++ {
							select {
							case <-closed:
							case <-time.After(5*time.Second - time.Since(started)):
								t.Fatal("revogação excedeu meta de 5s")
							}
						}
						if hubs[0].IsOnline(id) || hubs[1].IsOnline(id) {
							t.Fatal("principal permaneceu roteável")
						}
						machineRequest(t, servers[1], "POST", "/api/agent/output", c.Token, map[string]any{"instanceId": "missing"}, 401)
						t.Logf("%s observado nos dois nós em %s", action, time.Since(started))
					})
				}
			}
		})
	}
}
