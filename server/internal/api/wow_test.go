package api

// Diferenciais §5 wow — D-13 templates · D-14 self-service · D-15 quick actions.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/quickaction"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// ─── D-13 templates ─────────────────────────────────────────────────────────

// REGRA: o template é a FORMA do job — identidade e vínculos (id/team/upstream)
// são descartados no save; upsert por nome.
func TestTemplates_CRUDStripsIdentity(t *testing.T) {
	srv, _, _ := newDiffTestServer(t)

	body := `{"name":"etl-padrao","description":"ETL com retry",
	  "definition":{"id":"NAO-FICA","team":"NAO-FICA","label":"ETL","jobType":"COMMAND",
	    "retries":2,"schedule":{"enabled":true},"upstream":[{"from":"x","condition":"on-success"}]}}`
	resp, _ := doJSON(t, srv, "POST", "/api/templates", body)
	if resp.StatusCode != 200 {
		t.Fatalf("save template → %d", resp.StatusCode)
	}

	req := authReq(t, "GET", srv.URL+"/api/templates", "")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw := readBody(res)
	if !strings.Contains(raw, `"etl-padrao"`) || !strings.Contains(raw, `"retries":2`) {
		t.Fatalf("template deveria estar listado com a forma: %s", raw)
	}
	if strings.Contains(raw, "NAO-FICA") || strings.Contains(raw, `"upstream"`) {
		t.Fatalf("id/team/upstream do job de origem não podiam vazar pro molde: %s", raw)
	}

	// delete
	req = authReq(t, "DELETE", srv.URL+"/api/templates/etl-padrao", "")
	res2, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusNoContent {
		t.Fatalf("delete → %d", res2.StatusCode)
	}
}

// ─── D-14 self-service ──────────────────────────────────────────────────────

// harness com workspace REAL (defs em disco) — o gate lê def.SelfService.
func newSelfServiceServer(t *testing.T) (*httptest.Server, *storage.FileStore, *scheduler.Scheduler) {
	t.Helper()
	d := newTestDB(t)
	h := hub.New()
	store := storage.NewFileStore(t.TempDir(), false)
	sched := scheduler.New(store, d, h, time.Second)
	sched.DemoMode = true
	t.Cleanup(sched.Stop)
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Scheduler: sched, Store: store, Token: "test-token"}))
	t.Cleanup(func() { srv.Close(); d.Close() })
	return srv, store, sched
}

// REGRA: só jobs com selfService:true aparecem e rodam; job existente mas NÃO
// exposto responde 404 (o portal não vaza o catálogo).
func TestSelfService_GateByOptIn(t *testing.T) {
	srv, store, sched := newSelfServiceServer(t)
	if err := store.Save(domain.JobDefinition{
		ID: "exposto", Label: "Exposto", Team: "fin", JobType: "COMMAND",
		SelfService: true, Schedule: domain.Schedule{Enabled: true, Description: "roda relatório"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.JobDefinition{
		ID: "privado", Label: "Privado", Team: "fin", JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	// em produção o publish/webhook dispara o reload; aqui, direto.
	sched.ReloadDefs()

	req := authReq(t, "GET", srv.URL+"/api/selfservice/jobs", "")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw := readBody(res)
	if !strings.Contains(raw, "exposto") || strings.Contains(raw, "privado") {
		t.Fatalf("portal deveria listar SÓ o exposto: %s", raw)
	}

	resp, out := doJSON(t, srv, "POST", "/api/selfservice/run/exposto", "")
	if resp.StatusCode != 200 || out["instanceId"] == nil {
		t.Fatalf("run do exposto deveria ordenar, veio %d %+v", resp.StatusCode, out)
	}
	resp, _ = doJSON(t, srv, "POST", "/api/selfservice/run/privado", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("job não exposto deveria dar 404, veio %d", resp.StatusCode)
	}
}

// ─── D-15 quick actions ─────────────────────────────────────────────────────

// REGRA end-to-end: GET mostra confirmação SEM executar; POST executa (rerun
// NOTOK→WAITING); token adulterado = 403; sem login nenhum (o token É a auth).
func TestQuickAction_GetConfirmsPostExecutes(t *testing.T) {
	srv, d, _ := newDiffTestServer(t)
	today := time.Now().Format("2006-01-02")
	seedDiffInstance(t, d, "qa-1", "PIX", today, "NOTOK")

	secret, _ := quickaction.NewSecret()
	if _, err := d.Exec(`INSERT INTO settings(key,value) VALUES('quickaction_secret',?)`, secret); err != nil {
		t.Fatal(err)
	}
	tok, _ := quickaction.Sign(secret, "qa-1", "rerun", time.Now().Add(time.Hour))

	// GET (sem Authorization!) — página de confirmação, estado intacto
	res, err := srv.Client().Get(srv.URL + "/qa/" + tok)
	if err != nil {
		t.Fatal(err)
	}
	page := readBody(res)
	if res.StatusCode != 200 || !strings.Contains(page, "rerun") {
		t.Fatalf("GET deveria mostrar confirmação, veio %d %s", res.StatusCode, page)
	}
	var st string
	_ = d.QueryRow(`SELECT status FROM instances WHERE id='qa-1'`).Scan(&st)
	if st != "NOTOK" {
		t.Fatalf("GET não podia executar! status virou %s", st)
	}

	// POST — executa
	res2, err := srv.Client().Post(srv.URL+"/qa/"+tok, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	readBody(res2)
	if res2.StatusCode != 200 {
		t.Fatalf("POST → %d", res2.StatusCode)
	}
	_ = d.QueryRow(`SELECT status FROM instances WHERE id='qa-1'`).Scan(&st)
	if st != "WAITING" {
		t.Fatalf("rerun deveria deixar WAITING, veio %s", st)
	}

	// token adulterado → 403
	res3, _ := srv.Client().Post(srv.URL+"/qa/"+tok+"x", "", nil)
	readBody(res3)
	if res3.StatusCode != http.StatusForbidden {
		t.Fatalf("token adulterado deveria dar 403, veio %d", res3.StatusCode)
	}
}
