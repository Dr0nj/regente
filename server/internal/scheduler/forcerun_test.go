package scheduler

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
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

	// Sem force: a dep segura B em WAITING (gate WAIT_DEP).
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _ := carriedState(t, s, id); st != string(domain.StatusWaiting) {
		t.Fatalf("sem force a dep deveria segurar B em WAITING, está %s", st)
	}
	if ex, err := s.Explain(id); err != nil || hasKind(ex.Blockers, GateDep) == nil {
		t.Fatalf("Explain deveria apontar WAIT_DEP, veio %+v (err=%v)", ex.Blockers, err)
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
