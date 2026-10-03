package api

import (
	"encoding/json"
	"github.com/Dr0nj/regente-server/internal/db"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
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
