package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// O furo: GET /api/variables devolvia valor em claro para QUALQUER logado.
// Admin continua vendo; viewer/operator recebem o nome e a máscara.
func TestListVariables_ValorSoParaAdmin(t *testing.T) {
	database, err := db.Open(db.SQLite, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	varStore, err := storage.NewVariableStore(database)
	if err != nil {
		t.Fatalf("NewVariableStore: %v", err)
	}
	if _, err := varStore.Set("API_TOKEN", "s3cr3t-value", "admin"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	sched := scheduler.New(nil, database, nil, 0)
	sched.AttachVariables(varStore)
	srv := &server{cfg: Config{Scheduler: sched, DB: database}}

	for _, tc := range []struct {
		role     auth.Role
		wantsRaw bool
	}{
		{auth.RoleAdmin, true},
		{auth.RoleOperator, false},
		{auth.RoleViewer, false},
	} {
		r := httptest.NewRequest("GET", "/api/variables", nil)
		r = r.WithContext(auth.WithUser(r.Context(), &auth.User{Username: "u", Role: tc.role}))
		w := httptest.NewRecorder()
		srv.listVariables(w, r)

		body := w.Body.String()
		if !strings.Contains(body, "API_TOKEN") {
			t.Fatalf("role %v: o NOME tem que aparecer sempre (é o que se usa em %%%%NOME); body=%s", tc.role, body)
		}
		if got := strings.Contains(body, "s3cr3t-value"); got != tc.wantsRaw {
			t.Fatalf("role %v: valor em claro=%v, queria %v; body=%s", tc.role, got, tc.wantsRaw, body)
		}
		if !tc.wantsRaw && !strings.Contains(body, maskedVariableValue) {
			t.Fatalf("role %v: faltou a máscara; body=%s", tc.role, body)
		}
	}
}

// Sem usuário no contexto (não deveria acontecer atrás do authMiddleware, mas é
// o default que importa): mascara.
func TestListVariables_SemUsuarioMascara(t *testing.T) {
	database, err := db.Open(db.SQLite, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	varStore, err := storage.NewVariableStore(database)
	if err != nil {
		t.Fatalf("NewVariableStore: %v", err)
	}
	if _, err := varStore.Set("API_TOKEN", "s3cr3t-value", "admin"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	sched := scheduler.New(nil, database, nil, 0)
	sched.AttachVariables(varStore)
	srv := &server{cfg: Config{Scheduler: sched, DB: database}}

	r := httptest.NewRequest("GET", "/api/variables", nil)
	w := httptest.NewRecorder()
	srv.listVariables(w, r)
	if strings.Contains(w.Body.String(), "s3cr3t-value") {
		t.Fatalf("sem usuário no contexto tem que mascarar; body=%s", w.Body.String())
	}
}
