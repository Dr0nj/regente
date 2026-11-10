package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestReceiptMiddlewareDoesNotTurnCommitInto503(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	if err := d.EnableAudit(filepath.Join(t.TempDir(), "key"), 100); err != nil {
		t.Fatal(err)
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for range 10000 {
		if err = tx.AuditAdministrative("request.intent"); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	s := &server{cfg: Config{DB: d}}
	applied := false
	handler := s.scopedReceipt(func(scoped *server, w http.ResponseWriter, r *http.Request) {
		if !applied {
			if _, err := scoped.cfg.DB.Exec("INSERT INTO variables(name,value) VALUES('committed','yes')"); err != nil {
				t.Error(err)
				w.WriteHeader(503)
				return
			}
			applied = true
		}
		w.WriteHeader(200)
	})
	for range 20 {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/api/agent/v2/ack", nil))
		if w.Code != 200 {
			t.Fatal("commit virou falha de transporte", w.Code)
		}
	}
	var n int
	if err = d.QueryRow("SELECT COUNT(*) FROM security_audit").Scan(&n); err != nil || n != 10001 {
		t.Fatal("retry gerou intenções", n, err)
	}
	denied := s.scopedReceipt(func(_ *server, w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) })
	w := httptest.NewRecorder()
	denied(w, httptest.NewRequest("POST", "/api/agent/v2/result", nil))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if err = d.QueryRow("SELECT COUNT(*) FROM audit_delivery WHERE category='access'").Scan(&n); err != nil || n != 1 {
		t.Fatal("negação perdeu trilha", n, err)
	}
	if err = d.VerifyAudit(); err != nil {
		t.Fatal(err)
	}
}
