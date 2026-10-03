package api

import (
	"encoding/json"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestI14MandatoryAPI(t *testing.T) {
	srv, d := newOpsTestServer(t)
	if err := d.EnableAudit(filepath.Join(t.TempDir(), "signing.key"), 100); err != nil {
		t.Fatal(err)
	}
	c := srv.Client()
	response := doReq(t, c, http.MethodPost, srv.URL+"/api/users", "test-token", `{"username":"i14-user","password":"i14-secret-sentinel","role":"viewer"}`)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	records, err := d.AuditRecords(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range records {
		var payload db.AuditPayload
		if err = json.Unmarshal([]byte(r.Payload), &payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.Payload, "i14-secret-sentinel") || strings.Contains(r.Payload, "test-token") {
			t.Fatal("credencial exportada")
		}
		if len(payload.Mutations) > 0 && payload.Identity.Actor == "system" && payload.Identity.Route == "POST /api/users" {
			found = true
		}
		if payload.Identity.Request == "" {
			t.Fatal("requisição sem correlação")
		}
	}
	if !found {
		t.Fatal("usuário aceito sem auditoria atomicamente correlacionada")
	}
	operator := newOperatorToken(t, d, "i14-operator", nil)
	response = doReq(t, c, http.MethodGet, srv.URL+"/api/audit/security", operator, "")
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal(response.StatusCode)
	}
	records, err = d.AuditRecords(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, r := range records {
		var p db.AuditPayload
		_ = json.Unmarshal([]byte(r.Payload), &p)
		if p.Event == "access.denied" && p.Identity.Actor == "i14-operator" {
			found = true
		}
	}
	if !found {
		t.Fatal("negação de autorização sem trilha")
	}
	response = doReq(t, c, http.MethodPost, srv.URL+"/api/auth/login", "", `{"username":"absent","password":"i14-secret-sentinel"}`)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal(response.StatusCode)
	}
	if _, err = d.Raw().Exec("DROP TABLE audit_delivery"); err != nil {
		t.Fatal(err)
	}
	response = doReq(t, c, http.MethodPut, srv.URL+"/api/settings", "test-token", `{"env_label":"must-not-change"}`)
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal("não recusou antes da ação", response.StatusCode)
	}
	var count int
	if err = d.QueryRow("SELECT COUNT(*) FROM settings WHERE value='must-not-change'").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	response = doReq(t, c, http.MethodGet, srv.URL+"/api/users", "", "")
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal("negação sem storage retornou aceitação normal", response.StatusCode)
	}
}

func TestI14MemoryChangesWaitForAudit(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	if err := d.EnableAudit(filepath.Join(t.TempDir(), "audit.key"), 1); err != nil {
		t.Fatal(err)
	}
	gh, err := storage.NewGitHubClient("", "synthetic/repo", "")
	if err != nil {
		t.Fatal(err)
	}
	h := hub.New()
	store := storage.NewFileStore(t.TempDir(), false)
	sched := scheduler.New(store, d, h, time.Hour)
	defer sched.Stop()
	rt := scheduler.NewResourceTracker()
	sched.AttachResources(rt)
	if err = rt.LoadFromDB(d); err != nil {
		t.Fatal(err)
	}
	if err = rt.SetCapacity("audit-capacity", 2); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Store: store, Scheduler: sched, GitHub: gh, Token: "test-token"}))
	defer srv.Close()
	for _, endpoint := range []string{"/api/git/token", "/api/git/webhook-secret"} {
		response := doReq(t, srv.Client(), http.MethodPost, srv.URL+endpoint, "test-token", `{"token":"must-not-apply","secret":"must-not-apply"}`)
		response.Body.Close()
		if response.StatusCode < 500 {
			t.Fatal("alteração aceita com backlog cheio", endpoint, response.StatusCode)
		}
	}
	if gh.HasToken() || gh.HasWebhookSecret() {
		t.Fatal("credencial mudou sem commit auditado")
	}
	response := doReq(t, srv.Client(), http.MethodPut, srv.URL+"/api/resources/audit-capacity", "test-token", `{"capacity":7}`)
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal(response.StatusCode)
	}
	snapshot := rt.Snapshot()
	if len(snapshot) != 1 || snapshot[0].Capacity != 2 {
		t.Fatal("capacidade em memória mudou", snapshot)
	}
	if rt.TryAcquire("refused", map[string]int{"unknown": 1}) {
		t.Fatal("admitiu capacidade implícita sem auditoria")
	}
}
