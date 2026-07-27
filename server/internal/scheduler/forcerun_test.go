package scheduler

import (
	"sync"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
)

// "Run Now" sobre a instance EXISTENTE (POST /instances/{id}/force): um WAITING
// preso numa dep não-satisfeita, ao ser marcado forced=1 + reagendado pra agora
// (exatamente o UPDATE do handler forceRunInstance), passa a bypassar o gate de
// dep e é despachado pelo tick — SEM criar nenhuma instance nova. É o MESMO job.
func TestForceRun_ExistingInstanceBypassesDep_NoNewInstance(t *testing.T) {
	s := newTestScheduler(t) // DemoMode → mock-finish (OK em 1s), agente dispensado
	today := time.Now().Format("2006-01-02")

	// A fica RUNNING; B depende de A on-success → B trava em WAIT_DEP.
	defA := domain.JobDefinition{ID: "A", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	defB := domain.JobDefinition{
		ID: "B", JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "A", Condition: domain.CondOnSuccess}},
	}
	seedInst(t, s, "A-1", today, string(domain.StatusRunning), defA)
	id := seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB)

	// Sem force: a condição sintetizada A-TO-B não existe → WAIT_CONDITION.
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _ := carriedState(t, s, id); st != string(domain.StatusWaiting) {
		t.Fatalf("sem force a dep deveria segurar B em WAITING, está %s", st)
	}
	if ex, err := s.Explain(id); err != nil || hasKind(ex.Blockers, GateCondition) == nil {
		t.Fatalf("Explain deveria apontar WAIT_CONDITION, veio %+v (err=%v)", ex.Blockers, err)
	}

	before := countInstances(t, s)

	// "Run Now" na instance existente — o MESMO UPDATE que o handler executa.
	if _, err := s.db.Exec(
		`UPDATE instances SET status=?, forced=1, scheduled_at=? WHERE id=?`,
		string(domain.StatusWaiting), time.Now(), id,
	); err != nil {
		t.Fatalf("force-run update: %v", err)
	}

	// Agora o tick (ramo forced) bypassa a dep e despacha B.
	deadline := time.Now().Add(5 * time.Second)
	var st string
	for time.Now().Before(deadline) {
		s.Tick()
		_, st, _ = carriedState(t, s, id)
		if st == string(domain.StatusRunning) || st == string(domain.StatusOK) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("forced deveria bypassar a dep e ser despachado, está %s", st)
	}

	// A dep NÃO mudou (A continua RUNNING) — foi bypass puro, não satisfação.
	if _, st, _ := carriedState(t, s, "A-1"); st != string(domain.StatusRunning) {
		t.Fatalf("A deveria continuar RUNNING (bypass, não satisfação), está %s", st)
	}
	// E o principal: nenhuma nova instance foi criada.
	if after := countInstances(t, s); after != before {
		t.Fatalf("Run Now não pode criar nova instance: antes=%d depois=%d", before, after)
	}
}

// Run Now honra o gate de Confirm: um job confirm:true forçado NÃO dispara até o
// operador confirmar (paridade com o force-order — só o agente/dep é bypass).
func TestForceRun_DoesNotBypassConfirm(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "cf", JobType: "COMMAND", Confirm: true, Schedule: domain.Schedule{Enabled: true}}
	id := seedInst(t, s, "cf-1", today, string(domain.StatusWaiting), def)

	// force-run (o UPDATE do handler) — mas confirmed continua 0.
	if _, err := s.db.Exec(
		`UPDATE instances SET status=?, forced=1, scheduled_at=? WHERE id=?`,
		string(domain.StatusWaiting), time.Now(), id,
	); err != nil {
		t.Fatalf("force-run update: %v", err)
	}
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _ := carriedState(t, s, id); st != string(domain.StatusWaiting) {
		t.Fatalf("forced sem Confirm deve seguir WAITING, está %s", st)
	}
}

func countInstances(t *testing.T, s *Scheduler) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM instances`).Scan(&n); err != nil {
		t.Fatalf("count instances: %v", err)
	}
	return n
}

// === Ciclo de vida do dispatch da ordem forçada (flake da CI) ===
//
// O `run-job` do On/Do chama ForceOrder, que despacha em goroutine. Se essa
// goroutine não for rastreada pelo wg, ela escreve no DB DEPOIS do teardown do
// teste (Stop → Close → RemoveAll do t.TempDir) e a CI quebra com "TempDir
// RemoveAll cleanup: directory not empty" + "emitEvent .../started: sql:
// database is closed". Os dois testes abaixo fecham as duas pontas.

func eventCount(t *testing.T, s *Scheduler, instanceID, kind string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM instance_events WHERE instance_id=? AND kind=?`, instanceID, kind,
	).Scan(&n); err != nil {
		t.Fatalf("count events %s/%s: %v", instanceID, kind, err)
	}
	return n
}

func seedForceDef(t *testing.T, s *Scheduler, id string) {
	t.Helper()
	s.mu.Lock()
	s.defs = []domain.JobDefinition{{ID: id, Label: id, JobType: "COMMAND"}}
	s.mu.Unlock()
}

// blockingBus — segura a goroutine de dispatch no broadcast do RUNNING, que é a
// ÚLTIMA chamada da parte síncrona de startInstance ANTES do `wg.Add` interno.
// É exatamente a janela do vazamento: rastreada só por dentro, a goroutine ainda
// não contou no wg e o Stop() passa reto por ela.
type blockingBus struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (b *blockingBus) BroadcastWeb(event string, payload interface{}) {
	m, _ := payload.(map[string]string)
	if event != "instance.changed" || m["status"] != string(domain.StatusRunning) {
		return // o WAITING da criação é síncrono no ForceOrder — não pode travar
	}
	b.once.Do(func() { close(b.entered) })
	<-b.release
}
func (b *blockingBus) PickAgent(string, string) *hub.Client { return nil }
func (b *blockingBus) GetAgent(string) *hub.Client          { return nil }
func (b *blockingBus) HasAgent(string, string, string) bool { return true }
func (b *blockingBus) Dispatch(string, string, string, []byte) (hub.DispatchOutcome, string) {
	return hub.DispatchSent, "agent-1"
}

// Stop() só retorna depois que o dispatch da ordem forçada termina — é essa
// espera que garante que todo write da força aconteceu ANTES do Close do DB.
func TestForceOrder_StopWaitsForDispatch(t *testing.T) {
	s := newTestScheduler(t)
	bus := &blockingBus{entered: make(chan struct{}), release: make(chan struct{})}
	s.hub = bus
	seedForceDef(t, s, "cleanup")

	id, err := s.ForceOrder("cleanup")
	if err != nil {
		t.Fatalf("force: %v", err)
	}
	select {
	case <-bus.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("a ordem forçada deveria ter reivindicado a instance (broadcast do RUNNING)")
	}

	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
		t.Fatal("Stop() retornou com o dispatch da ordem forçada ainda em voo (goroutine não rastreada)")
	case <-time.After(100 * time.Millisecond):
	}

	close(bus.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() não retornou depois do dispatch terminar")
	}
	// Os writes do dispatch já estão no DB — nada pendente para depois do Close.
	if n := eventCount(t, s, id, "submitted"); n != 1 {
		t.Fatalf("o `submitted` do dispatch deveria estar gravado antes do Stop retornar, veio %d", n)
	}
}

// Depois do Stop() a ordem forçada não ABRE trabalho novo: a instance é criada
// (INSERT síncrono) mas o dispatch não nasce — nenhuma goroutine escapa para
// escrever durante o RemoveAll do t.TempDir().
func TestForceOrder_AfterStop_DoesNotDispatch(t *testing.T) {
	s := newTestScheduler(t)
	seedForceDef(t, s, "cleanup")
	s.Stop() // teardown em curso; o t.Cleanup(s.Stop) roda de novo (idempotente)

	id, err := s.ForceOrder("cleanup")
	if err != nil {
		t.Fatalf("force: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // janela de sobra para uma goroutine solta agir

	var st string
	if err := s.db.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&st); err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != string(domain.StatusWaiting) {
		t.Fatalf("depois do Stop a ordem forçada deveria ficar WAITING (sem dispatch), está %s", st)
	}
	if n := eventCount(t, s, id, "started"); n != 0 {
		t.Fatalf("nenhum dispatch deveria ter começado depois do Stop, veio %d evento(s) `started`", n)
	}
}
