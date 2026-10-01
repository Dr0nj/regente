package api

import (
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProductionIdentity(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			const pw = "production-fixture-password"
			if err := auth.BootstrapProduction(d, "admin", pw, ""); err != nil {
				t.Fatal(err)
			}
			policy := runtimeprofile.Config{Profile: "production", Environment: "prod", ControlPlane: "deny"}
			if err := runtimeprofile.Bind(d, policy); err != nil {
				t.Fatal(err)
			}
			token, _, err := auth.Login(d, "admin", pw)
			if err != nil {
				t.Fatal(err)
			}
			h := hub.New()
			store := storage.NewFileStore(t.TempDir(), false)
			sched := scheduler.New(store, d, h, time.Hour)
			t.Cleanup(sched.Stop)
			cfg := Config{DB: d, Hub: h, Store: store, Scheduler: sched, Token: "test-token", RuntimePolicy: policy}
			srv := httptest.NewServer(NewRouter(cfg))
			defer srv.Close()
			machineRequest(t, srv, "GET", "/api/users", "test-token", nil, 401)
			machineRequest(t, srv, "GET", "/api/users", token, nil, 200)
			machineRequest(t, srv, "PUT", "/api/settings", token, map[string]string{"_runtime_environment": "staging"}, 400)
			machineRequest(t, srv, "POST", "/api/users", token, map[string]string{"username": "weak", "password": "short", "role": "viewer"}, 400)
			for _, env := range []string{"", "staging", "prod"} {
				want := 400
				if env == "prod" {
					want = 200
				}
				b := tokenRequest{AgentID: "worker" + env, Environment: env, Capabilities: []string{"COMMAND"}, ExpiresAt: time.Now().Add(time.Hour)}
				raw := machineRequest(t, srv, "POST", "/api/agents/tokens", token, b, want)
				if want == 200 {
					var issued issuedMachine
					if err := json.Unmarshal(raw, &issued); err != nil {
						t.Fatal(err)
					}
					s := server{cfg: cfg}
					r := httptest.NewRequest("GET", "/", nil)
					r.Header.Set("Authorization", "Bearer "+issued.Token)
					p, ok := s.machineAuth(r)
					if !ok || !s.machineValid(p) {
						t.Fatal("escopo válido recusado")
					}
					p.Environment = ""
					if s.machineValid(p) {
						t.Fatal("escopo vazio aceito")
					}
				}
			}
			// Credencial legada válida no banco continua recusada na borda produtiva.
			tx, err := d.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec("INSERT INTO machine_principals(agent_id,environment,capabilities) VALUES('legacy','','COMMAND')"); err != nil {
				t.Fatal(err)
			}
			issued, err := issueMachineToken(tx, tokenRequest{AgentID: "legacy", Capabilities: []string{"COMMAND"}, ExpiresAt: time.Now().Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			s := server{cfg: cfg}
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Authorization", "Bearer "+issued["token"].(string))
			if _, ok := s.machineAuth(r); ok {
				t.Fatal("token legado aceito")
			}
			for _, path := range []string{"/ws/agent?id=legacy&caps=COMMAND", "/api/agent/poll?id=legacy&caps=COMMAND", "/api/agent/events?id=legacy&caps=COMMAND"} {
				machineRequest(t, srv, "GET", path, issued["token"].(string), nil, 401)
			}
			machineRequest(t, srv, "POST", fmt.Sprintf("/api/agents/tokens/%v/rotate", issued["id"]), token, map[string]any{"expiresAt": time.Now().Add(time.Hour), "graceSeconds": 0}, 400)
			if _, err := s.webAccess(auth.Digest("test-token")); err == nil {
				t.Fatal("token estático aceito em eventos")
			}
			cfg.AuthMode = "oidc"
			oidcSrv := httptest.NewServer(NewRouter(cfg))
			defer oidcSrv.Close()
			machineRequest(t, oidcSrv, "POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": pw}, 403)
		})
	}
}
