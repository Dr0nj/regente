package api

import (
	"net/http/httptest"
	"testing"

	"github.com/Dr0nj/regente-server/internal/hub"
)

// Event log: seed de instances + eventos, e verifica o feed do dia, o filtro por
// kind e a paginação por cursor.
func TestEventLog_FeedFilterAndCursor(t *testing.T) {
	d := newTestDB(t)
	date := "2026-06-23"
	seedInstances(t, d, date) // job-1..job-6 em BATCH/PIX

	// Seed determinístico de eventos (id cresce na ordem de inserção).
	seed := []struct {
		inst, kind, actor string
	}{
		{"job-1-" + date, "ordered", "scheduler"},
		{"job-1-" + date, "started", "scheduler"},
		{"job-1-" + date, "finished", "agent"},
		{"job-2-" + date, "ordered", "scheduler"},
		{"job-2-" + date, "held", "operator"},
		{"job-3-" + date, "finished", "agent"},
	}
	for _, e := range seed {
		if _, err := d.Exec(`INSERT INTO instance_events(instance_id, kind, actor) VALUES(?,?,?)`, e.inst, e.kind, e.actor); err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}

	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token"}))
	defer func() { srv.Close(); d.Close() }()

	// 1) feed do dia = todos os 6 eventos.
	var all struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"nextCursor"`
	}
	getJSON(t, srv, "/api/events?date="+date, &all)
	if len(all.Items) != 6 {
		t.Fatalf("feed do dia esperava 6 eventos, veio %d", len(all.Items))
	}
	// ordem desc por id → o mais recente primeiro.
	if all.Items[0]["kind"] != "finished" || all.Items[0]["instanceId"] != "job-3-"+date {
		t.Errorf("primeiro evento (mais recente) inesperado: %+v", all.Items[0])
	}
	// join trouxe o team.
	if all.Items[0]["team"] != "BATCH" {
		t.Errorf("evento deveria carregar team da instance, veio %v", all.Items[0]["team"])
	}

	// 2) filtro por kind=finished → só os 2 finished.
	var fin struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, srv, "/api/events?date="+date+"&kind=finished", &fin)
	if len(fin.Items) != 2 {
		t.Fatalf("kind=finished esperava 2, veio %d", len(fin.Items))
	}

	// 3) filtro por folder=PIX (job-4,5,6) — nenhum evento seedado nelas → 0.
	var pix struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, srv, "/api/events?date="+date+"&folder=PIX", &pix)
	if len(pix.Items) != 0 {
		t.Fatalf("folder=PIX sem eventos esperava 0, veio %d", len(pix.Items))
	}

	// 4) paginação: limit=4 → 4 itens + cursor; 2ª página traz o resto.
	var p1 struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"nextCursor"`
	}
	getJSON(t, srv, "/api/events?date="+date+"&limit=4", &p1)
	if len(p1.Items) != 4 || p1.NextCursor == "" {
		t.Fatalf("página 1 esperava 4 itens + cursor, veio %d cursor=%q", len(p1.Items), p1.NextCursor)
	}
	var p2 struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"nextCursor"`
	}
	getJSON(t, srv, "/api/events?date="+date+"&limit=4&cursor="+p1.NextCursor, &p2)
	if len(p2.Items) != 2 || p2.NextCursor != "" {
		t.Fatalf("página 2 esperava 2 itens e fim, veio %d cursor=%q", len(p2.Items), p2.NextCursor)
	}
}
