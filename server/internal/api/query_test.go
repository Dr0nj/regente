package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// Parser de intenção (função pura): cobre summary, list por status, count, explain,
// extração de folder e o fallback "unknown".
func TestParseQueryIntent(t *testing.T) {
	cases := []struct {
		q          string
		wantKind   string
		wantStatus string
		wantFolder string
		wantJobRef string
	}{
		{"resumo do dia", "summary", "", "", ""},
		{"como está hoje?", "summary", "", "", ""},
		{"o que falhou hoje", "list", "NOTOK", "", ""},
		{"jobs bloqueados na folder PIX", "list", "WAITING", "PIX", ""},
		{"quais jobs estão rodando", "list", "RUNNING", "", ""},
		{"quantos falharam", "count", "NOTOK", "", ""},
		{"quantos jobs na pasta BATCH", "count", "", "BATCH", ""},
		{"por que o job carga_diaria não rodou?", "explain", "", "", "carga_diaria"},
		{"por que carga não rodou", "explain", "", "", "carga"}, // job de palavra única, antes do verbo
		{"why did extract fail", "explain", "", "", "extract"},  // EN: <job> fail
		{"por que não rodou", "unknown", "", "", ""},            // sem job → não chuta
		{"me diz um negócio aleatório", "unknown", "", "", ""},
	}
	for _, c := range cases {
		got := parseQueryIntent(c.q)
		if got.Kind != c.wantKind {
			t.Errorf("%q: kind=%q, esperava %q (%+v)", c.q, got.Kind, c.wantKind, got)
		}
		if got.Status != c.wantStatus {
			t.Errorf("%q: status=%q, esperava %q", c.q, got.Status, c.wantStatus)
		}
		if got.Folder != c.wantFolder {
			t.Errorf("%q: folder=%q, esperava %q", c.q, got.Folder, c.wantFolder)
		}
		if got.JobRef != c.wantJobRef {
			t.Errorf("%q: jobRef=%q, esperava %q", c.q, got.JobRef, c.wantJobRef)
		}
	}
}

// End-to-end: POST /api/query contra HOJE (o endpoint sempre consulta o dia atual).
func TestRunQuery_EndToEnd(t *testing.T) {
	d := newTestDB(t)
	today := time.Now().Format("2006-01-02")
	// seed do dia atual: 1 NOTOK + 1 OK + 1 WAITING na folder BATCH.
	rows := []struct{ id, status string }{
		{"hoje-notok-" + today, "NOTOK"},
		{"hoje-ok-" + today, "OK"},
		{"hoje-wait-" + today, "WAITING"},
	}
	for _, r := range rows {
		if _, err := d.Exec(
			`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at) VALUES(?,?,?,?,?,CURRENT_TIMESTAMP)`,
			r.id, r.id, "BATCH", today, r.status,
		); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	h := hub.New()
	sched := scheduler.New(storage.NewFileStore(t.TempDir(), false), d, h, time.Second)
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Scheduler: sched, Token: "test-token"}))
	defer func() { srv.Close(); d.Close() }()

	post := func(q string) map[string]any {
		raw, _ := json.Marshal(map[string]string{"q": q})
		req := authReq(t, "POST", srv.URL+"/api/query", string(raw))
		resp, err := srv.Client().Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("POST query %q → %v err=%v", q, statusOf(resp), err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}
	answer := func(out map[string]any) map[string]any { a, _ := out["answer"].(map[string]any); return a }

	// resumo → texto + total 3.
	out := post("resumo do dia")
	if k := out["interpreted"].(map[string]any)["kind"]; k != "summary" {
		t.Fatalf("interpretação deveria ser summary, veio %v", k)
	}
	if a := answer(out); a == nil || !strings.Contains(a["text"].(string), "3 jobs") {
		t.Fatalf("resumo esperava '3 jobs', veio %+v", out["answer"])
	}

	// "o que falhou" → 1 NOTOK.
	if a := answer(post("o que falhou hoje")); a["count"].(float64) != 1 {
		t.Errorf("esperava 1 NOTOK, veio %v", a["count"])
	}

	// "quantos rodando" → 0 (nenhum RUNNING seedado).
	if a := answer(post("quantos rodando")); a["count"].(float64) != 0 {
		t.Errorf("esperava 0 RUNNING, veio %v", a["count"])
	}

	// explain de um job existente → texto não-vazio + instanceId resolvido.
	a := answer(post("por que hoje-notok não rodou"))
	if a["instanceId"] == nil || a["text"] == "" {
		t.Errorf("explain deveria resolver o job e trazer summary, veio %+v", a)
	}

	// pergunta sem sentido → unknown com sugestão.
	a = answer(post("qual a capital da frança"))
	if !strings.Contains(a["text"].(string), "didn't understand") {
		t.Errorf("pergunta fora de escopo deveria cair no fallback, veio %+v", a)
	}
}
