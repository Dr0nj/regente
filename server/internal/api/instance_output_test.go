package api

// OL-2 — a API do sysout (GET /instances/{id}/output) e a exclusão do kind=output
// legado dos leitores de auditoria (events / feed do dia).

import (
	"net/http/httptest"
	"testing"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
)

func mustExecAPI(t *testing.T, d *db.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func TestGetInstanceOutput_PerAttempt(t *testing.T) {
	d := newTestDB(t)
	date := "2026-06-23"
	seedInstances(t, d, date) // job-2 = RUNNING, job-1 = OK
	id := "job-2-" + date
	// 2 tentativas: a 1 (falha, preservada pela OL-1), a 2 (corrente).
	mustExecAPI(t, d, `UPDATE instances SET attempts=2, exit_code=0 WHERE id=?`, id)
	mustExecAPI(t, d, `INSERT INTO instance_output(instance_id, attempt, chunk) VALUES(?,1,?)`, id, "falhou tentativa 1\n")
	mustExecAPI(t, d, `INSERT INTO instance_output(instance_id, attempt, chunk) VALUES(?,2,?)`, id, "linha A\n")
	mustExecAPI(t, d, `INSERT INTO instance_output(instance_id, attempt, chunk) VALUES(?,2,?)`, id, "linha B\n")

	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token"}))
	defer func() { srv.Close(); d.Close() }()

	// Sem attempt → tentativa CORRENTE (2), live-tail concatenado.
	var cur struct {
		Attempts int    `json:"attempts"`
		Attempt  int    `json:"attempt"`
		Text     string `json:"text"`
		Complete bool   `json:"complete"`
	}
	getJSON(t, srv, "/api/instances/"+id+"/output", &cur)
	if cur.Attempts != 2 || cur.Attempt != 2 {
		t.Fatalf("esperava attempts=2 attempt=2, veio %+v", cur)
	}
	if cur.Text != "linha A\nlinha B\n" {
		t.Fatalf("concat da tentativa corrente errado: %q", cur.Text)
	}
	if cur.Complete {
		t.Fatalf("RUNNING não deveria estar complete")
	}

	// attempt=1 → o sysout da tentativa ANTERIOR (preservado), e complete (superada).
	var prev struct {
		Attempt  int    `json:"attempt"`
		Text     string `json:"text"`
		Complete bool   `json:"complete"`
	}
	getJSON(t, srv, "/api/instances/"+id+"/output?attempt=1", &prev)
	if prev.Attempt != 1 || prev.Text != "falhou tentativa 1\n" || !prev.Complete {
		t.Fatalf("tentativa 1 inesperada: %+v", prev)
	}
}

// Fallback: instance TERMINADA sem chunks (pré-OL) serve o instances.output.
func TestGetInstanceOutput_FallbackConsolidado(t *testing.T) {
	d := newTestDB(t)
	date := "2026-06-23"
	seedInstances(t, d, date)
	id := "job-1-" + date // OK
	mustExecAPI(t, d, `UPDATE instances SET output='saida consolidada', exit_code=0 WHERE id=?`, id)

	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token"}))
	defer func() { srv.Close(); d.Close() }()

	var out struct {
		Text     string `json:"text"`
		Complete bool   `json:"complete"`
		ExitCode *int   `json:"exitCode"`
	}
	getJSON(t, srv, "/api/instances/"+id+"/output", &out)
	if out.Text != "saida consolidada" || !out.Complete {
		t.Fatalf("fallback consolidado falhou: %+v", out)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Fatalf("exitCode deveria vir na tentativa final terminada: %+v", out.ExitCode)
	}
}

// OL-2 — o kind=output LEGADO some do jornal de agendamento (events do instance
// e feed do dia) por default; ?include=output o traz de volta.
func TestEvents_ExcludeLegacyOutput(t *testing.T) {
	d := newTestDB(t)
	date := "2026-06-23"
	seedInstances(t, d, date)
	id := "job-1-" + date
	mustExecAPI(t, d, `INSERT INTO instance_events(instance_id, kind, actor) VALUES(?,'started','scheduler')`, id)
	mustExecAPI(t, d, `INSERT INTO instance_events(instance_id, kind, actor, message) VALUES(?,'output','agent','ruído de sysout')`, id)
	mustExecAPI(t, d, `INSERT INTO instance_events(instance_id, kind, actor) VALUES(?,'finished','agent')`, id)

	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token"}))
	defer func() { srv.Close(); d.Close() }()

	// /instances/{id}/events — sem include: sem o kind=output.
	var evs []map[string]any
	getJSON(t, srv, "/api/instances/"+id+"/events", &evs)
	if len(evs) != 2 {
		t.Fatalf("Logs deveria esconder kind=output; veio %d eventos", len(evs))
	}
	for _, e := range evs {
		if e["kind"] == "output" {
			t.Fatalf("kind=output vazou pro Logs: %+v", e)
		}
	}
	// include=output traz o histórico legado.
	var withOut []map[string]any
	getJSON(t, srv, "/api/instances/"+id+"/events?include=output", &withOut)
	if len(withOut) != 3 {
		t.Fatalf("?include=output deveria trazer os 3; veio %d", len(withOut))
	}

	// Feed do dia (/api/events) — mesma exclusão.
	var feed struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, srv, "/api/events?date="+date, &feed)
	for _, e := range feed.Items {
		if e["kind"] == "output" {
			t.Fatalf("kind=output vazou pro feed do dia: %+v", e)
		}
	}
}
