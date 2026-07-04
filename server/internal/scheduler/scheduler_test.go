package scheduler

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// Fase A — imutabilidade: defForInstance deve preferir o snapshot congelado e
// só cair na def viva quando não há snapshot (instances legadas).
func TestDefForInstance_PrefersSnapshot(t *testing.T) {
	live := map[string]domain.JobDefinition{"x": {ID: "x", Label: "LIVE"}}

	snap, _ := json.Marshal(domain.JobDefinition{ID: "x", Label: "FROZEN"})
	d, ok := defForInstance(instRow{DefID: "x", Snapshot: string(snap)}, live)
	if !ok || d.Label != "FROZEN" {
		t.Fatalf("esperava snapshot FROZEN, veio ok=%v label=%q", ok, d.Label)
	}

	d2, ok2 := defForInstance(instRow{DefID: "x"}, live)
	if !ok2 || d2.Label != "LIVE" {
		t.Fatalf("sem snapshot deveria cair na def viva LIVE, veio ok=%v label=%q", ok2, d2.Label)
	}

	if _, ok3 := defForInstance(instRow{DefID: "inexistente"}, live); ok3 {
		t.Fatal("def inexistente sem snapshot deveria retornar ok=false")
	}
}

// newTestScheduler — Scheduler real sobre SQLite num arquivo temporário.
func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	// DSN sem WAL (busy_timeout explícito → sqliteDSN respeita e não força WAL):
	// no modo demo o mock de dispatch escreve numa goroutine que pode terminar
	// depois do Close do teste; com WAL sobra -wal/-shm e o RemoveAll do t.TempDir()
	// falha ("directory not empty"). Journal DELETE é limpo por commit.
	database, err := db.Open(db.SQLite, filepath.Join(t.TempDir(), "t.db")+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := New(storage.NewFileStore(t.TempDir(), false), database, hub.New(), 10*time.Millisecond)
	// Testes rodam sem agente conectado: DemoMode mantém o mock-finish (OK em 1s),
	// que é o comportamento que os cenários de dispatch/retry/alerting assumem.
	s.DemoMode = true
	// Para as goroutines de fundo (mock-finish/retry) ANTES do Close do DB (LIFO:
	// registrado depois do Close → roda antes dele). Sem isso o mock-finish de 1s
	// escrevia no DB durante o RemoveAll do t.TempDir() → flake "directory not
	// empty" na CI (TestTick_BlockedSuccessorWaitsAndRunsAfterSetOK et al.).
	t.Cleanup(s.Stop)
	return s
}

// R2 — watchdog: Tick() deve carimbar lastTickAt (loop de scheduling vivo).
func TestTick_UpdatesLastTick(t *testing.T) {
	s := newTestScheduler(t)
	if !s.LastTick().IsZero() {
		t.Fatal("lastTick deveria começar zero")
	}
	s.Tick()
	if s.LastTick().IsZero() {
		t.Fatal("Tick() deveria atualizar lastTick")
	}
	if age := time.Since(s.LastTick()); age > time.Second {
		t.Fatalf("lastTick deveria ser recente, idade=%v", age)
	}
}

// panicBus — Bus cujo Dispatch explode, para exercitar o recover do dispatch.
type panicBus struct{}

func (panicBus) BroadcastWeb(string, interface{}) {}
func (panicBus) PickAgent(string) *hub.Client     { return nil }
func (panicBus) GetAgent(string) *hub.Client      { return nil }
func (panicBus) HasAgent(string, string) bool     { return true }
func (panicBus) Dispatch(string, string, []byte) (hub.DispatchOutcome, string) {
	panic("boom no Dispatch")
}

// R2 — panic-recovery: um panic na goroutine de dispatch NÃO derruba o processo
// e a instance é finalizada como NOTOK (não fica pendurada em RUNNING).
func TestDispatch_PanicRecovered(t *testing.T) {
	s := newTestScheduler(t)
	s.hub = panicBus{} // injeta o Bus que explode no PickAgent

	def := domain.JobDefinition{ID: "p", Label: "Panic", JobType: "COMMAND"}
	snap, _ := json.Marshal(def)
	id := "p-2026-01-01"
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot) VALUES(?,?,?,?,?,?)`,
		id, "p", "2026-01-01", string(domain.StatusWaiting), time.Now(), string(snap),
	); err != nil {
		t.Fatalf("insert instance: %v", err)
	}

	s.startInstance(id, def) // se o recover falhar, o teste (processo) crasha aqui

	// O dispatch roda em goroutine; espera a transição terminal.
	deadline := time.Now().Add(2 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		_ = s.db.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status)
		if status == string(domain.StatusNotOK) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status != string(domain.StatusNotOK) {
		t.Fatalf("após panic no dispatch a instance deveria ser NOTOK, veio %q", status)
	}
}

// Paridade Control-M (2026-07-02): sucessor com pai NOTOK/RUNNING fica WAITING
// ("Wait Event") — o tick NÃO cancela mais quando a dep é "permanentemente"
// impossível, porque o flip é reversível na operação (rerun/Set OK no pai).
// Set OK no pai destrava a aresta e o PRÓPRIO tick despacha o sucessor.
func TestTick_BlockedSuccessorWaitsAndRunsAfterSetOK(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")

	parent := domain.JobDefinition{ID: "pai", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	child := domain.JobDefinition{
		ID: "filho", JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "pai", Condition: domain.CondOnSuccess}},
	}
	runningParent := domain.JobDefinition{ID: "pai2", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	child2 := domain.JobDefinition{
		ID: "filho2", JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "pai2", Condition: domain.CondOnSuccess}},
	}
	seedInst(t, s, "pai-1", today, string(domain.StatusNotOK), parent)
	seedInst(t, s, "filho-1", today, string(domain.StatusWaiting), child)
	seedInst(t, s, "pai2-1", today, string(domain.StatusRunning), runningParent)
	seedInst(t, s, "filho2-1", today, string(domain.StatusWaiting), child2)

	s.Tick()
	if _, st, _, _ := carriedState(t, s, "filho-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("sucessor de pai NOTOK deve permanecer WAITING (Wait Event), está %s", st)
	}
	if _, st, _, _ := carriedState(t, s, "filho2-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("sucessor de pai RUNNING deve permanecer WAITING (Wait Event), está %s", st)
	}

	// Operador: Set OK no pai → aresta on-success satisfeita → tick despacha o filho.
	if err := s.SetOK("pai-1"); err != nil {
		t.Fatalf("set-ok: %v", err)
	}
	s.Tick()
	if _, st, _, _ := carriedState(t, s, "filho-1"); st == string(domain.StatusWaiting) || st == string(domain.StatusCancelled) {
		t.Fatalf("após Set OK no pai o filho deveria despachar, está %s", st)
	}
}

// daily_at configurável em runtime via settings: valor válido vence o default;
// inválido/ausente cai no default do processo.
func TestDailyAt_FromSettings(t *testing.T) {
	s := newTestScheduler(t)
	if got := s.DailyAt(); got != "00:00" {
		t.Fatalf("default esperado 00:00, veio %s", got)
	}
	if _, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES('daily_at','03:30')`); err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	if got := s.DailyAt(); got != "03:30" {
		t.Fatalf("esperado 03:30 do settings, veio %s", got)
	}
	if _, err := s.db.Exec(`UPDATE settings SET value='25:99' WHERE key='daily_at'`); err != nil {
		t.Fatalf("update setting: %v", err)
	}
	if got := s.DailyAt(); got != "00:00" {
		t.Fatalf("valor inválido deve cair no default 00:00, veio %s", got)
	}
}

// Sem agente e sem demo-mode: o tick NÃO reivindica (nada de RUNNING↔WAITING
// piscando a cada tick, sem spam de evento) — o job fica WAITING ("WAIT AGENT")
// e o Explain diz o porquê. Forced idem.
func TestTick_NoAgentNoClaim(t *testing.T) {
	s := newTestScheduler(t)
	s.DemoMode = false
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "na", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedInst(t, s, "na-1", today, string(domain.StatusWaiting), def)
	forced := domain.JobDefinition{ID: "naf", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedInst(t, s, "naf-1", today, string(domain.StatusWaiting), forced)
	if _, err := s.db.Exec(`UPDATE instances SET forced=1 WHERE id='naf-1'`); err != nil {
		t.Fatalf("mark forced: %v", err)
	}

	for i := 0; i < 4; i++ {
		s.Tick()
	}
	if _, st, _, _ := carriedState(t, s, "na-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("sem agente o job deve permanecer WAITING, está %s", st)
	}
	if _, st, _, _ := carriedState(t, s, "naf-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("forced sem agente também deve permanecer WAITING, está %s", st)
	}
	// Nenhum claim = nenhum evento "started".
	var started int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id IN ('na-1','naf-1') AND kind='started'`).Scan(&started)
	if started != 0 {
		t.Fatalf("claim indevido sem agente: %d eventos started", started)
	}
	// Throttle: no máximo 1 evento de no-agent por instance apesar de 4 ticks.
	var evs int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id='na-1'`).Scan(&evs)
	if evs > 1 {
		t.Fatalf("spam de eventos sem agente: %d (esperado <=1)", evs)
	}
	// Explain expõe o motivo (WAIT_AGENT) — fonte única.
	ex, err := s.Explain("na-1")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if hasKind(ex.Blockers, GateAgent) == nil {
		t.Fatalf("Explain deveria apontar WAIT_AGENT, veio %+v", ex.Blockers)
	}
}

// Imutabilidade do Monitoring (bug 👻GHOST, 2026-07-04): a flag dryRun de uma
// instância JÁ ORDENADA é congelada na ordem (coluna dry_run, schemaV9) e NÃO
// pode ser reescrita ao mexer na definition viva. Antes, o front derivava o selo
// GHOST da def viva — ligar dryRun no Design + publicar acendia o selo em cards de
// jobs já materializados em tempo real, o que o Monitoring nunca deve fazer.
func TestDryRun_FrozenOnOrder_NotRewrittenByLiveDef(t *testing.T) {
	s := newTestScheduler(t)
	s.DemoMode = false // só interessa a materialização (INSERT), não o dispatch.

	// Def SEM dryRun no momento da ordem.
	def := domain.JobDefinition{ID: "gh", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}, DryRun: false}
	s.mu.Lock()
	s.defs = []domain.JobDefinition{def}
	s.mu.Unlock()

	const date = "2026-07-04"
	if created := s.RunDaily(date); created != 1 {
		t.Fatalf("esperava 1 instance materializada, veio %d", created)
	}

	readDryRun := func() int {
		var dr int
		if err := s.db.QueryRow(`SELECT COALESCE(dry_run,0) FROM instances WHERE definition_id='gh' AND order_date=?`, date).Scan(&dr); err != nil {
			t.Fatalf("read dry_run: %v", err)
		}
		return dr
	}

	if dr := readDryRun(); dr != 0 {
		t.Fatalf("instância nasceu com dry_run=%d, esperava 0 (def não era dryRun na ordem)", dr)
	}

	// Simula "ligar dryRun no Design e publicar": a def VIVA muda...
	s.mu.Lock()
	s.defs = []domain.JobDefinition{{ID: "gh", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}, DryRun: true}}
	s.mu.Unlock()

	// ...mas a instância JÁ ORDENADA continua imutável (só a próxima ordem reflete).
	if dr := readDryRun(); dr != 0 {
		t.Fatalf("mexer na def viva reescreveu a instância (dry_run=%d) — Monitoring deveria ser imutável", dr)
	}

	// A PRÓXIMA ordem (força manual) sim carrega o novo valor congelado.
	if _, err := s.ForceOrder("gh"); err != nil {
		t.Fatalf("force: %v", err)
	}
	var forcedDR int
	if err := s.db.QueryRow(`SELECT COALESCE(dry_run,0) FROM instances WHERE definition_id='gh' AND forced=1`).Scan(&forcedDR); err != nil {
		t.Fatalf("read forced dry_run: %v", err)
	}
	if forcedDR != 1 {
		t.Fatalf("nova ordem (force) deveria congelar dry_run=1, veio %d", forcedDR)
	}
}
