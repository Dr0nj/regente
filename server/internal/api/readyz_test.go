package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Dr0nj/regente-server/internal/db"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(db.SQLite, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

// R3 — /readyz responde 200 + ready:true quando o DB está alcançável.
func TestReadyz_OK(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	s := &server{cfg: Config{DB: d}}

	rec := httptest.NewRecorder()
	s.readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json inválido: %v", err)
	}
	if body["ready"] != true {
		t.Fatalf("esperava ready=true, body=%s", rec.Body.String())
	}
	if dbc, _ := body["db"].(map[string]any); dbc["ok"] != true {
		t.Fatalf("esperava db.ok=true, body=%s", rec.Body.String())
	}
}

// R3 — DB inalcançável → 503 + ready:false (k8s tira o pod de rotação).
func TestReadyz_DBDown(t *testing.T) {
	d := newTestDB(t)
	d.Close() // fecha a conexão → PingContext falha
	s := &server{cfg: Config{DB: d}}

	rec := httptest.NewRecorder()
	s.readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("esperava 503 com DB fechado, veio %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["ready"] != false {
		t.Fatalf("esperava ready=false, body=%s", rec.Body.String())
	}
}
