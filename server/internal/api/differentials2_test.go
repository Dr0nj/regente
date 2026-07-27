package api

// Diferenciais leva 2 (2026-07-07) — D-2 pause/resume · D-3 ingest externo ·
// D-5 query estruturada · D-11 chaos inject. Cada teste declara a REGRA que
// protege, não a implementação.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// harness mínimo: DB + scheduler + router autenticado por token legado.
func newDiffTestServer(t *testing.T) (*httptest.Server, *db.DB, *scheduler.Scheduler) {
	t.Helper()
	d := newTestDB(t)
	h := hub.New()
	sched := scheduler.New(storage.NewFileStore(t.TempDir(), false), d, h, time.Second)
	sched.AttachConditions(scheduler.NewConditionEngine(d))
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Scheduler: sched, Token: "test-token"}))
	t.Cleanup(func() { srv.Close(); d.Close() })
	t.Cleanup(sched.Stop) // LIFO: drena as goroutines do scheduler ANTES do Close do DB
	return srv, d, sched
}

func seedDiffInstance(t *testing.T, d *db.DB, id, team, date, status string) {
	t.Helper()
	if _, err := d.Exec(
		`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at) VALUES(?,?,?,?,?,CURRENT_TIMESTAMP)`,
		id, id, team, date, status,
	); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func doJSON(t *testing.T, srv *httptest.Server, method, path, body string) (*http.Response, map[string]any) {
	t.Helper()
	req := authReq(t, method, srv.URL+path, body)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

// ─── D-3 ingest de eventos externos ─────────────────────────────────────────

// REGRA: o retry do emissor (mesmo id) NÃO re-aplica o efeito — idempotência
// é o que separa "event-driven confiável" de um webhook qualquer.
func TestIngest_IdempotentByEventID(t *testing.T) {
	srv, d, _ := newDiffTestServer(t)
	today := time.Now().Format("2006-01-02")
	body := `{"id":"evt-42","source":"sap","conditions":["ARQ_OK"]}`

	resp, out := doJSON(t, srv, "POST", "/api/events/ingest", body)
	if resp.StatusCode != 200 || out["duplicate"] != false {
		t.Fatalf("1º ingest deveria aplicar, veio %v %+v", resp.StatusCode, out)
	}
	resp, out = doJSON(t, srv, "POST", "/api/events/ingest", body)
	if resp.StatusCode != 200 || out["duplicate"] != true {
		t.Fatalf("2º ingest (retry) deveria ser duplicate, veio %v %+v", resp.StatusCode, out)
	}
	// a condition existe UMA vez, no escopo de hoje
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM conditions WHERE name='ARQ_OK' AND scope_date=?`, today).Scan(&n)
	if n != 1 {
		t.Fatalf("condition deveria existir exatamente 1x, veio %d", n)
	}
}

// REGRA: evento sem condition nem forceJob é rejeitado (400) — ingestão vazia
// esconderia um emissor mal configurado.
func TestIngest_RejectsEmptyEffect(t *testing.T) {
	srv, _, _ := newDiffTestServer(t)
	resp, _ := doJSON(t, srv, "POST", "/api/events/ingest", `{"id":"evt-1","source":"x"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ingest sem efeito deveria dar 400, veio %d", resp.StatusCode)
	}
}

// ─── D-5 query estruturada ──────────────────────────────────────────────────

func TestStructuredQuery_FiltersAndGroupBy(t *testing.T) {
	srv, d, _ := newDiffTestServer(t)
	today := time.Now().Format("2006-01-02")
	seedDiffInstance(t, d, "q-a1", "PIX", today, "NOTOK")
	seedDiffInstance(t, d, "q-a2", "PIX", today, "OK")
	seedDiffInstance(t, d, "q-b1", "CORE", today, "NOTOK")

	// filtro composto: folders IN + statuses IN
	resp, out := doJSON(t, srv, "POST", "/api/instances/query",
		`{"folders":["PIX"],"statuses":["NOTOK"]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("query → %d", resp.StatusCode)
	}
	items := out["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != "q-a1" {
		t.Fatalf("esperava só q-a1, veio %+v", items)
	}

	// agregação groupBy status (contadores no banco, sem materializar linhas)
	_, out = doJSON(t, srv, "POST", "/api/instances/query", `{"groupBy":"status"}`)
	if int(out["total"].(float64)) != 3 {
		t.Fatalf("groupBy total deveria ser 3, veio %v", out["total"])
	}

	// parse ESTRITO: campo desconhecido = 400 (não silêncio)
	resp, _ = doJSON(t, srv, "POST", "/api/instances/query", `{"folderz":["PIX"]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("campo desconhecido deveria dar 400, veio %d", resp.StatusCode)
	}
}

// REGRA (decisão de transporte registrada no roadmap): o método HTTP QUERY é
// aceito na MESMA handler do POST — progressive enhancement, mesma resposta.
func TestStructuredQuery_QueryVerb(t *testing.T) {
	srv, d, _ := newDiffTestServer(t)
	today := time.Now().Format("2006-01-02")
	seedDiffInstance(t, d, "qv-1", "PIX", today, "WAITING")

	resp, out := doJSON(t, srv, "QUERY", "/api/instances/query", `{"folders":["PIX"]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("QUERY verb → %d (esperava 200)", resp.StatusCode)
	}
	if items := out["items"].([]any); len(items) != 1 {
		t.Fatalf("QUERY deveria devolver 1 item, veio %d", len(items))
	}
}

// ─── D-2 pause/resume de workflow ───────────────────────────────────────────

// REGRA: pause segura os WAITING da folder (e SÓ os WAITING); resume devolve.
// Estado da instance (attempts/cycle_runs/scheduled_at) fica intacto — é pausa,
// não reset.
func TestFolderPauseResume_PreservesState(t *testing.T) {
	srv, d, _ := newDiffTestServer(t)
	today := time.Now().Format("2006-01-02")
	seedDiffInstance(t, d, "w-1", "PIX", today, "WAITING")
	seedDiffInstance(t, d, "w-2", "PIX", today, "RUNNING")
	seedDiffInstance(t, d, "w-3", "CORE", today, "WAITING")
	// estado rico: um cyclic na volta 3, retry na tentativa 2
	if _, err := d.Exec(`UPDATE instances SET cycle_runs=3, attempts=2 WHERE id='w-1'`); err != nil {
		t.Fatal(err)
	}

	resp, out := doJSON(t, srv, "POST", "/api/folders/PIX/pause", "")
	if resp.StatusCode != 200 || int(out["affected"].(float64)) != 1 {
		t.Fatalf("pause deveria afetar 1 (só o WAITING da PIX), veio %v %+v", resp.StatusCode, out)
	}
	var st string
	var cycles, attempts int
	_ = d.QueryRow(`SELECT status, cycle_runs, attempts FROM instances WHERE id='w-1'`).Scan(&st, &cycles, &attempts)
	if st != "HELD" || cycles != 3 || attempts != 2 {
		t.Fatalf("pause deveria dar HELD preservando cycle_runs/attempts, veio %s %d %d", st, cycles, attempts)
	}
	// RUNNING e a outra folder não foram tocados
	_ = d.QueryRow(`SELECT status FROM instances WHERE id='w-2'`).Scan(&st)
	if st != "RUNNING" {
		t.Fatalf("RUNNING não é pausável, veio %s", st)
	}
	_ = d.QueryRow(`SELECT status FROM instances WHERE id='w-3'`).Scan(&st)
	if st != "WAITING" {
		t.Fatalf("outra folder não podia mudar, veio %s", st)
	}

	// resume: HELD→WAITING com o estado ainda lá
	_, out = doJSON(t, srv, "POST", "/api/folders/PIX/resume", "")
	if int(out["affected"].(float64)) != 1 {
		t.Fatalf("resume deveria afetar 1, veio %+v", out)
	}
	_ = d.QueryRow(`SELECT status, cycle_runs FROM instances WHERE id='w-1'`).Scan(&st, &cycles)
	if st != "WAITING" || cycles != 3 {
		t.Fatalf("resume deveria voltar WAITING com cycle_runs=3, veio %s %d", st, cycles)
	}
	// trilha de auditoria: eventos paused/resumed registrados na instance
	var evts int
	_ = d.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id='w-1' AND kind IN ('paused','resumed')`).Scan(&evts)
	if evts != 2 {
		t.Fatalf("esperava eventos paused+resumed, veio %d", evts)
	}
}

// REGRA (schemaV14): um job segurado pela PAUSA DE FOLDER (hold_scope='folder')
// não pode ser liberado individualmente (409); só o resume da folder o destrava.
// Um hold individual (hold_scope='') é liberável 1-a-1 e SOBREVIVE ao resume da
// folder — os dois escopos não se misturam.
func TestFolderHold_BlocksIndividualRelease(t *testing.T) {
	srv, d, _ := newDiffTestServer(t)
	today := time.Now().Format("2006-01-02")
	seedDiffInstance(t, d, "f-1", "PIX", today, "WAITING") // vira folder-held
	seedDiffInstance(t, d, "f-2", "PIX", today, "WAITING") // hold individual antes da pausa

	// hold individual em f-2 (fica HELD scope='')
	if resp, _ := doJSON(t, srv, "POST", "/api/instances/f-2/hold", ""); resp.StatusCode != 200 {
		t.Fatalf("hold individual deveria funcionar, veio %d", resp.StatusCode)
	}
	// pausa a folder: só f-1 (o WAITING) é afetado — f-2 já está HELD
	if _, out := doJSON(t, srv, "POST", "/api/folders/PIX/pause", ""); int(out["affected"].(float64)) != 1 {
		t.Fatalf("pause deveria segurar só f-1 (o WAITING), veio %+v", out)
	}
	var scope1, scope2 string
	_ = d.QueryRow(`SELECT COALESCE(hold_scope,'') FROM instances WHERE id='f-1'`).Scan(&scope1)
	_ = d.QueryRow(`SELECT COALESCE(hold_scope,'') FROM instances WHERE id='f-2'`).Scan(&scope2)
	if scope1 != "folder" || scope2 != "" {
		t.Fatalf("f-1 devia ser folder-held e f-2 individual, veio %q %q", scope1, scope2)
	}

	// Release individual no folder-held f-1 → 409 e continua HELD
	resp, _ := doJSON(t, srv, "POST", "/api/instances/f-1/release", "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("release individual de folder-held deveria dar 409, veio %d", resp.StatusCode)
	}
	var st string
	_ = d.QueryRow(`SELECT status FROM instances WHERE id='f-1'`).Scan(&st)
	if st != "HELD" {
		t.Fatalf("f-1 devia continuar HELD após o 409, veio %s", st)
	}

	// summary reporta PIX como paused enquanto houver folder-held
	_, sum := doJSON(t, srv, "GET", "/api/instances/summary?date="+today, "")
	paused, _ := sum["pausedFolders"].([]any)
	if len(paused) != 1 || paused[0] != "PIX" {
		t.Fatalf("summary devia listar PIX em pausedFolders, veio %+v", sum["pausedFolders"])
	}

	// resume da folder: destrava SÓ f-1 (folder); f-2 (individual) continua HELD
	if _, out := doJSON(t, srv, "POST", "/api/folders/PIX/resume", ""); int(out["affected"].(float64)) != 1 {
		t.Fatalf("resume deveria destravar só f-1, veio %+v", out)
	}
	_ = d.QueryRow(`SELECT status FROM instances WHERE id='f-1'`).Scan(&st)
	if st != "WAITING" {
		t.Fatalf("f-1 devia voltar WAITING após resume, veio %s", st)
	}
	_ = d.QueryRow(`SELECT status FROM instances WHERE id='f-2'`).Scan(&st)
	if st != "HELD" {
		t.Fatalf("f-2 (hold individual) devia sobreviver ao resume da folder, veio %s", st)
	}
	// agora o Release individual de f-2 funciona (scope '')
	if resp, _ := doJSON(t, srv, "POST", "/api/instances/f-2/release", ""); resp.StatusCode != 200 {
		t.Fatalf("release individual de hold individual deveria funcionar, veio %d", resp.StatusCode)
	}
	// e a folder some do pausedFolders
	_, sum = doJSON(t, srv, "GET", "/api/instances/summary?date="+today, "")
	if paused, _ := sum["pausedFolders"].([]any); len(paused) != 0 {
		t.Fatalf("sem folder-held, pausedFolders devia ser vazio, veio %+v", sum["pausedFolders"])
	}
}

// ─── D-11 chaos inject ──────────────────────────────────────────────────────

// REGRA: inject-failure derruba WAITING/RUNNING pelo fluxo REAL (evento chaos +
// NOTOK); um job já terminado não é injetável (400).
func TestInjectFailure(t *testing.T) {
	srv, d, _ := newDiffTestServer(t)
	today := time.Now().Format("2006-01-02")
	seedDiffInstance(t, d, "c-1", "PIX", today, "RUNNING")
	seedDiffInstance(t, d, "c-2", "PIX", today, "OK")

	resp, out := doJSON(t, srv, "POST", "/api/instances/c-1/inject-failure", "")
	if resp.StatusCode != 200 || out["injected"] != true {
		t.Fatalf("inject em RUNNING deveria funcionar, veio %d %+v", resp.StatusCode, out)
	}
	var st, output string
	_ = d.QueryRow(`SELECT status, COALESCE(output,'') FROM instances WHERE id='c-1'`).Scan(&st, &output)
	if st != "NOTOK" || !strings.Contains(output, "chaos") {
		t.Fatalf("esperava NOTOK com output de chaos, veio %s %q", st, output)
	}
	var evts int
	_ = d.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id='c-1' AND kind='chaos'`).Scan(&evts)
	if evts != 1 {
		t.Fatalf("esperava 1 evento chaos, veio %d", evts)
	}

	resp, _ = doJSON(t, srv, "POST", "/api/instances/c-2/inject-failure", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("inject em OK deveria dar 400, veio %d", resp.StatusCode)
	}
}
