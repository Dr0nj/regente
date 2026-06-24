package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
)

// P2/escala — read-path. Seed determinístico: 6 instances em 2 folders × 3 status,
// scheduled_at crescente para ordenação estável.
func seedInstances(t *testing.T, d *db.DB, date string) {
	t.Helper()
	base := time.Date(2026, 6, 23, 8, 0, 0, 0, time.UTC)
	rows := []struct {
		id, team, status string
	}{
		{"job-1-" + date, "BATCH", "OK"},
		{"job-2-" + date, "BATCH", "RUNNING"},
		{"job-3-" + date, "BATCH", "WAITING"},
		{"job-4-" + date, "PIX", "OK"},
		{"job-5-" + date, "PIX", "WAITING"},
		{"job-6-" + date, "PIX", "WAITING"},
	}
	for i, r := range rows {
		if _, err := d.Exec(
			`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at) VALUES(?,?,?,?,?,?)`,
			r.id, r.id, r.team, date, r.status, base.Add(time.Duration(i)*time.Minute),
		); err != nil {
			t.Fatalf("seed %s: %v", r.id, err)
		}
	}
}

func scaleServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	d := newTestDB(t)
	date := "2026-06-23"
	seedInstances(t, d, date)
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token"}))
	t.Cleanup(func() { srv.Close(); d.Close() })
	return srv, date
}

func getJSON(t *testing.T, srv *httptest.Server, path string, out any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s → %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// Filtro server-side: folder, status, busca — tudo resolvido NO BANCO.
func TestListInstances_ServerSideFilters(t *testing.T) {
	srv, date := scaleServer(t)

	var all []instanceRow
	getJSON(t, srv, "/api/instances?date="+date, &all)
	if len(all) != 6 {
		t.Fatalf("sem filtro esperava 6, veio %d", len(all))
	}

	var batch []instanceRow
	getJSON(t, srv, "/api/instances?date="+date+"&folder=BATCH", &batch)
	if len(batch) != 3 {
		t.Fatalf("folder=BATCH esperava 3, veio %d", len(batch))
	}
	for _, ir := range batch {
		if ir.Team != "BATCH" {
			t.Fatalf("folder=BATCH trouxe team=%q", ir.Team)
		}
	}

	var waiting []instanceRow
	getJSON(t, srv, "/api/instances?date="+date+"&status=WAITING", &waiting)
	if len(waiting) != 3 {
		t.Fatalf("status=WAITING esperava 3, veio %d", len(waiting))
	}

	var pixWaiting []instanceRow
	getJSON(t, srv, "/api/instances?date="+date+"&folder=PIX&status=WAITING", &pixWaiting)
	if len(pixWaiting) != 2 {
		t.Fatalf("PIX+WAITING esperava 2, veio %d", len(pixWaiting))
	}

	var search []instanceRow
	getJSON(t, srv, "/api/instances?date="+date+"&q=job-4", &search)
	if len(search) != 1 || search[0].ID != "job-4-"+date {
		t.Fatalf("q=job-4 esperava [job-4], veio %v", search)
	}

	var multi []instanceRow
	getJSON(t, srv, "/api/instances?date="+date+"&status=OK,RUNNING", &multi)
	if len(multi) != 3 { // 2 OK + 1 RUNNING
		t.Fatalf("status=OK,RUNNING esperava 3, veio %d", len(multi))
	}
}

// Contadores agregados (dashboard) sem baixar linha nenhuma.
func TestSummaryInstances(t *testing.T) {
	srv, date := scaleServer(t)
	var sum struct {
		Total    int            `json:"total"`
		ByStatus map[string]int `json:"byStatus"`
		ByFolder map[string]int `json:"byFolder"`
	}
	getJSON(t, srv, "/api/instances/summary?date="+date, &sum)
	if sum.Total != 6 {
		t.Fatalf("total esperava 6, veio %d", sum.Total)
	}
	if sum.ByStatus["WAITING"] != 3 || sum.ByStatus["OK"] != 2 || sum.ByStatus["RUNNING"] != 1 {
		t.Fatalf("byStatus errado: %v", sum.ByStatus)
	}
	if sum.ByFolder["BATCH"] != 3 || sum.ByFolder["PIX"] != 3 {
		t.Fatalf("byFolder errado: %v", sum.ByFolder)
	}
	// Summary respeita filtro (só PIX). Var nova: json.Decode mescla em map existente.
	var pix struct {
		Total    int            `json:"total"`
		ByFolder map[string]int `json:"byFolder"`
	}
	getJSON(t, srv, "/api/instances/summary?date="+date+"&folder=PIX", &pix)
	if pix.Total != 3 || pix.ByFolder["BATCH"] != 0 || pix.ByFolder["PIX"] != 3 {
		t.Fatalf("summary filtrado por PIX errado: total=%d byFolder=%v", pix.Total, pix.ByFolder)
	}
}

// Paginação por cursor: caminha em páginas de 2 e tem de reconstruir o conjunto
// inteiro, em ordem, sem repetir nem pular.
func TestPageInstances_CursorWalk(t *testing.T) {
	srv, date := scaleServer(t)
	seen := []string{}
	cursor := ""
	for pages := 0; pages < 100; pages++ {
		var page struct {
			Items      []instanceRow `json:"items"`
			NextCursor string        `json:"nextCursor"`
		}
		path := fmt.Sprintf("/api/instances/page?date=%s&limit=2", date)
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		getJSON(t, srv, path, &page)
		if len(page.Items) == 0 {
			break
		}
		if len(page.Items) > 2 {
			t.Fatalf("limit=2 mas a página veio com %d", len(page.Items))
		}
		for _, ir := range page.Items {
			seen = append(seen, ir.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 6 {
		t.Fatalf("cursor walk esperava 6 ids, veio %d (%v)", len(seen), seen)
	}
	// Ordem estável por scheduled_at + sem duplicatas.
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("ordem/duplicata quebrada: %v", seen)
		}
	}
}

// Unit do builder de WHERE + placeholders (puro, determinístico).
func TestInstanceQuery_Where(t *testing.T) {
	q := instanceQuery{date: "2026-06-23", folder: "PIX", statuses: []string{"OK", "WAITING"}, search: "job"}
	where, args := q.where([]string{"PIX", "BATCH"}, true)
	// order_date + team + status IN(2) + LIKE(2) + team IN(2) = 1+1+2+2+2 = 8 args
	if len(args) != 8 {
		t.Fatalf("esperava 8 args, veio %d (%v)", len(args), args)
	}
	for _, frag := range []string{"order_date=?", "team=?", "status IN (?,?)", "id LIKE ?", "team IN (?,?)"} {
		if !contains(where, frag) {
			t.Fatalf("WHERE não contém %q: %s", frag, where)
		}
	}
	// restrict + allowed vazio = não vê nada.
	w2, _ := (instanceQuery{date: "d"}).where(nil, true)
	if !contains(w2, "1=0") {
		t.Fatalf("restrict sem folders deveria forçar 1=0: %s", w2)
	}
	if placeholders(3) != "?,?,?" || placeholders(0) != "" {
		t.Fatalf("placeholders errado: %q / %q", placeholders(3), placeholders(0))
	}
}

// Benchmark do read-path: prova que summary/page têm custo BOUNDED mesmo com o dia
// inteiro grande (o full-fetch antigo cresce linear). Env-gated:
// REGENTE_SCALE_N=100000 go test ./internal/api -run TestReadPath_Scale -v
func TestReadPath_Scale(t *testing.T) {
	raw := os.Getenv("REGENTE_SCALE_N")
	if raw == "" {
		t.Skip("defina REGENTE_SCALE_N (ex.: 100000) para o benchmark do read-path")
	}
	n, _ := strconv.Atoi(raw)
	d := newTestDB(t)
	date := "2026-06-23"
	base := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)

	// Seed em lote (transação) — n instances espalhadas em 100 folders × 4 status.
	statuses := []string{"WAITING", "RUNNING", "OK", "NOTOK"}
	tx, _ := d.Begin()
	st, _ := tx.Prepare(`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at) VALUES(?,?,?,?,?,?)`)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("job-%07d-%s", i, date)
		team := fmt.Sprintf("folder-%02d", i%100)
		_, _ = st.Exec(id, id, team, date, statuses[i%4], base.Add(time.Duration(i)*time.Second))
	}
	st.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token"}))
	defer srv.Close()
	defer d.Close()

	// summary (GROUP BY) sobre n linhas.
	t0 := time.Now()
	var sum struct {
		Total int `json:"total"`
	}
	getJSON(t, srv, "/api/instances/summary?date="+date, &sum)
	tSummary := time.Since(t0)

	// 1ª página de 500 (folder filtrado).
	t0 = time.Now()
	var page struct {
		Items []instanceRow `json:"items"`
	}
	getJSON(t, srv, "/api/instances/page?date="+date+"&limit=500&folder=folder-07", &page)
	tPage := time.Since(t0)

	// full-fetch antigo (baixa o dia inteiro) — pra contraste.
	t0 = time.Now()
	var all []instanceRow
	getJSON(t, srv, "/api/instances?date="+date, &all)
	tFull := time.Since(t0)

	if sum.Total != n {
		t.Fatalf("summary total=%d, esperava %d", sum.Total, n)
	}
	t.Logf("READ-PATH @ %d instances → summary=%v · page(500,folder)=%v · full-fetch(dia inteiro, %d linhas)=%v",
		n, tSummary.Round(time.Millisecond), tPage.Round(time.Millisecond), len(all), tFull.Round(time.Millisecond))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
