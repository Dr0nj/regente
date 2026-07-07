package api

// E3 — RBAC de escrita por AÇÃO OPERACIONAL (folder-scoped): hold/release/
// cancel/rerun/set-ok/confirm e o bulk exigem write na folder DONA da instance
// (coluna `team` snapshotada), além do writer role. Bearer legado = admin.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// newOpsTestServer — router real com Scheduler+Store (os handlers de ação
// emitem eventos e o fallback de folder lê a def viva).
func newOpsTestServer(t *testing.T) (*httptest.Server, *db.DB) {
	t.Helper()
	d := newTestDB(t)
	t.Cleanup(func() { _ = d.Close() })
	h := hub.New()
	store := storage.NewFileStore(t.TempDir(), false)
	sched := scheduler.New(store, d, h, time.Hour)
	t.Cleanup(sched.Stop)
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Token: "test-token", Scheduler: sched, Store: store}))
	t.Cleanup(srv.Close)
	return srv, d
}

func seedInstance(t *testing.T, d *db.DB, id, team, status string) {
	t.Helper()
	if _, err := d.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, team) VALUES(?,?,?,?,?,?)`,
		id, "def-"+id, "2026-07-07", status, time.Now(), team,
	); err != nil {
		t.Fatalf("seed instance %s: %v", id, err)
	}
}

// newOperatorToken cria um operator (com ACLs opcionais) e devolve o token de sessão.
func newOperatorToken(t *testing.T, d *db.DB, name string, acls map[string]string) string {
	t.Helper()
	u, err := auth.CreateUser(d, name, "pw", auth.RoleOperator)
	if err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	for folder, perms := range acls {
		if err := auth.SetUserACL(d, u.ID, folder, perms); err != nil {
			t.Fatalf("acl %s/%s: %v", name, folder, err)
		}
	}
	tok, _, err := auth.Login(d, name, "pw")
	if err != nil {
		t.Fatalf("login %s: %v", name, err)
	}
	return tok
}

func doReq(t *testing.T, c *http.Client, method, url, token, body string) *http.Response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// Aceite E3: operator com ACL write=[FIN] consegue rerun em instance FIN e
// leva 403 em instance RISCO; job solto (team='') segue o modo do user
// (irrestrito pode, restrito não); bearer legado bypassa (admin).
func TestRBACOps_RerunFolderScoped(t *testing.T) {
	srv, d := newOpsTestServer(t)
	c := srv.Client()
	seedInstance(t, d, "fin-1", "FIN", "NOTOK")
	seedInstance(t, d, "risco-1", "RISCO", "NOTOK")
	seedInstance(t, d, "solto-1", "", "NOTOK")

	opFIN := newOperatorToken(t, d, "opfin", map[string]string{"FIN": "rw"})
	opLivre := newOperatorToken(t, d, "oplivre", nil)

	cases := []struct {
		name, token, instance string
		want                  int
	}{
		{"opfin pode na FIN", opFIN, "fin-1", 200},
		{"opfin 403 na RISCO", opFIN, "risco-1", 403},
		{"opfin (restrito) 403 no job solto", opFIN, "solto-1", 403},
		{"operator irrestrito pode no job solto", opLivre, "solto-1", 200},
		{"bearer legado (admin) bypassa", "test-token", "risco-1", 200},
	}
	for _, tc := range cases {
		resp := doReq(t, c, http.MethodPost, srv.URL+"/api/instances/"+tc.instance+"/rerun", tc.token, "")
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("%s: esperava %d, veio %d", tc.name, tc.want, resp.StatusCode)
		}
	}
}

// Demais ações unitárias passam pelo MESMO gate (hold aqui como amostra do
// caminho requireInstanceWrite; todas chamam o mesmo helper).
func TestRBACOps_HoldUsaMesmoGate(t *testing.T) {
	srv, d := newOpsTestServer(t)
	c := srv.Client()
	seedInstance(t, d, "risco-2", "RISCO", "WAITING")
	opFIN := newOperatorToken(t, d, "opfin", map[string]string{"FIN": "rw"})

	resp := doReq(t, c, http.MethodPost, srv.URL+"/api/instances/risco-2/hold", opFIN, "")
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("hold em folder sem write deveria dar 403, veio %d", resp.StatusCode)
	}
}

// Aceite E3: bulk misto NÃO aborta o lote — reporta ok/failed POR ITEM.
func TestRBACOps_BulkReporta403PorItem(t *testing.T) {
	srv, d := newOpsTestServer(t)
	c := srv.Client()
	seedInstance(t, d, "fin-1", "FIN", "NOTOK")
	seedInstance(t, d, "risco-1", "RISCO", "NOTOK")
	opFIN := newOperatorToken(t, d, "opfin", map[string]string{"FIN": "rw"})

	resp := doReq(t, c, http.MethodPost, srv.URL+"/api/bulk/instances", opFIN,
		`{"action":"rerun","ids":["fin-1","risco-1"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("bulk deveria responder 200 (resultado por item), veio %d", resp.StatusCode)
	}
	var out bulkResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Ok != 1 || out.Failed != 1 {
		t.Fatalf("esperava ok=1 failed=1, veio ok=%d failed=%d", out.Ok, out.Failed)
	}
	for _, r := range out.Results {
		switch r.ID {
		case "fin-1":
			if !r.OK {
				t.Fatalf("fin-1 deveria passar: %s", r.Error)
			}
		case "risco-1":
			if r.OK || !strings.Contains(r.Error, "RISCO") {
				t.Fatalf("risco-1 deveria falhar citando a folder, veio ok=%v err=%q", r.OK, r.Error)
			}
		}
	}
}
