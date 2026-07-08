package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// ---------------------------------------------------------------------------
// Ciclo de vida da daily (carry-over entre diárias, Control-M New Day).
// ---------------------------------------------------------------------------

// def helper com keepActive embutido no schedule.
func defKeep(id string, keepActive int) domain.JobDefinition {
	return domain.JobDefinition{
		ID: id, JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true, KeepActive: keepActive},
	}
}

// seedInst insere uma instance num order_date com snapshot da def (carry_budget=-1
// por default). Retorna o id.
func seedInst(t *testing.T, s *Scheduler, id, date, status string, def domain.JobDefinition) string {
	t.Helper()
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot) VALUES(?,?,?,?,?,?)`,
		id, def.ID, date, status, time.Now(), string(snap),
	); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	return id
}

// carriedState lê o estado de carry de uma instance.
func carriedState(t *testing.T, s *Scheduler, id string) (orderDate, status, from string, budget int) {
	t.Helper()
	err := s.db.QueryRow(
		`SELECT order_date, status, COALESCE(carried_from,''), COALESCE(carry_budget,-1) FROM instances WHERE id=?`, id,
	).Scan(&orderDate, &status, &from, &budget)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return
}

// TestCarryDecision — a REGRA pura, sem banco. Cobre as 5 classes de estado.
func TestCarryDecision(t *testing.T) {
	plain := defKeep("a", 0)
	keep3 := defKeep("a", 3)

	cases := []struct {
		name      string
		status    string
		budget    int
		attempts  int
		def       domain.JobDefinition
		wantCarry bool
		wantBudg  int
	}{
		{"running sempre carrega", string(domain.StatusRunning), -1, 1, plain, true, -1},
		{"held sempre carrega", string(domain.StatusHeld), -1, 1, plain, true, -1},
		{"notok default inicia em 1 e carrega", string(domain.StatusNotOK), -1, 1, plain, true, 0},
		{"notok com budget esgotado nao carrega", string(domain.StatusNotOK), 0, 1, plain, false, 0},
		{"notok keepActive=3 inicia em 3", string(domain.StatusNotOK), -1, 1, keep3, true, 2},
		{"notok keepActive consome budget", string(domain.StatusNotOK), 2, 1, keep3, true, 1},
		{"waiting sem keepActive nao carrega", string(domain.StatusWaiting), -1, 1, plain, false, 0},
		{"waiting keepActive=3 carrega", string(domain.StatusWaiting), -1, 1, keep3, true, 2},
		// D-1 — WAITING com attempts>1 é um RETRY AGENDADO em andamento: carrega
		// com a regra do NOTOK (baseline 1 sem keepActive), senão um retryDelayMin
		// de dias morreria na primeira virada da daily.
		{"waiting retry-pendente carrega como notok", string(domain.StatusWaiting), -1, 2, plain, true, 0},
		{"waiting retry-pendente com budget esgotado nao carrega", string(domain.StatusWaiting), 0, 2, plain, false, 0},
		{"waiting retry-pendente keepActive=3 inicia em 3", string(domain.StatusWaiting), -1, 2, keep3, true, 2},
		{"ok nunca carrega", string(domain.StatusOK), -1, 1, plain, false, -1},
		{"cancelled nunca carrega", string(domain.StatusCancelled), -1, 1, plain, false, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := carryDecision(c.status, c.budget, c.attempts, c.def)
			if got.carry != c.wantCarry {
				t.Fatalf("carry=%v, esperava %v (reason=%s)", got.carry, c.wantCarry, got.reason)
			}
			if got.carry && got.newBudget != c.wantBudg {
				t.Fatalf("newBudget=%d, esperava %d", got.newBudget, c.wantBudg)
			}
		})
	}
}

// TestCarryOver_RunningPersists — REGRA: um job RUNNING na virada NÃO some; segue
// na daily de hoje (mesmo id/status), com carried_from = diária de origem.
func TestCarryOver_RunningPersists(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "r-23", "2026-06-23", string(domain.StatusRunning), defKeep("r", 0))

	if created := s.RunDaily("2026-06-24"); created != 0 {
		t.Fatalf("sem defs habilitadas, RunDaily deveria criar 0 frescas, criou %d", created)
	}
	od, st, from, _ := carriedState(t, s, "r-23")
	if od != "2026-06-24" {
		t.Fatalf("RUNNING deveria avançar pra 2026-06-24, ficou em %s", od)
	}
	if st != string(domain.StatusRunning) {
		t.Fatalf("status deveria seguir RUNNING, veio %s", st)
	}
	if from != "2026-06-23" {
		t.Fatalf("carried_from deveria ser 2026-06-23, veio %s", from)
	}
}

// TestCarryOver_HeldPersists — REGRA: HELD atravessa as diárias enquanto em hold.
func TestCarryOver_HeldPersists(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "h-23", "2026-06-23", string(domain.StatusHeld), defKeep("h", 0))

	s.RunDaily("2026-06-24")
	if od, st, _, _ := carriedState(t, s, "h-23"); od != "2026-06-24" || st != string(domain.StatusHeld) {
		t.Fatalf("HELD deveria persistir em 24, veio order=%s status=%s", od, st)
	}
	// Continua em hold → atravessa a próxima virada também.
	s.RunDaily("2026-06-25")
	if od, _, _, _ := carriedState(t, s, "h-23"); od != "2026-06-25" {
		t.Fatalf("HELD deveria seguir pra 25, ficou em %s", od)
	}
}

// TestCarryOver_NotOKDefaultOneDay — DEFAULT: NOTOK não-tratado persiste +1 diária,
// depois para (budget esgota).
func TestCarryOver_NotOKDefaultOneDay(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "x-23", "2026-06-23", string(domain.StatusNotOK), defKeep("x", 0))

	s.RunDaily("2026-06-24")
	if od, _, _, b := carriedState(t, s, "x-23"); od != "2026-06-24" || b != 0 {
		t.Fatalf("NOTOK default deveria ir pra 24 com budget 0, veio order=%s budget=%d", od, b)
	}
	// Segunda virada: budget esgotado → NÃO carrega.
	s.RunDaily("2026-06-25")
	if od, _, _, _ := carriedState(t, s, "x-23"); od != "2026-06-24" {
		t.Fatalf("NOTOK com budget esgotado NÃO deveria ir pra 25, foi pra %s", od)
	}
}

// TestCarryOver_KeepActiveSurvivesNDays — keepActive=2 sobrevive 2 diárias extras.
func TestCarryOver_KeepActiveSurvivesNDays(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "y-23", "2026-06-23", string(domain.StatusNotOK), defKeep("y", 2))

	s.RunDaily("2026-06-24") // budget 2 -> 1
	if od, _, _, b := carriedState(t, s, "y-23"); od != "2026-06-24" || b != 1 {
		t.Fatalf("dia 24: esperava order=24 budget=1, veio order=%s budget=%d", od, b)
	}
	s.RunDaily("2026-06-25") // budget 1 -> 0
	if od, _, _, b := carriedState(t, s, "y-23"); od != "2026-06-25" || b != 0 {
		t.Fatalf("dia 25: esperava order=25 budget=0, veio order=%s budget=%d", od, b)
	}
	s.RunDaily("2026-06-26") // budget 0 -> para
	if od, _, _, _ := carriedState(t, s, "y-23"); od != "2026-06-25" {
		t.Fatalf("dia 26: keepActive=2 esgotado, NÃO deveria ir além de 25, foi pra %s", od)
	}
}

// TestCarryOver_OKAndCancelledDoNotCarry — encerrados não atravessam a virada.
func TestCarryOver_OKAndCancelledDoNotCarry(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "ok-23", "2026-06-23", string(domain.StatusOK), defKeep("ok", 5))
	seedInst(t, s, "cx-23", "2026-06-23", string(domain.StatusCancelled), defKeep("cx", 5))

	s.RunDaily("2026-06-24")
	if od, _, _, _ := carriedState(t, s, "ok-23"); od != "2026-06-23" {
		t.Fatalf("OK não deveria carregar, foi pra %s", od)
	}
	if od, _, _, _ := carriedState(t, s, "cx-23"); od != "2026-06-23" {
		t.Fatalf("CANCELLED não deveria carregar, foi pra %s", od)
	}
}

// TestCarryOver_DoesNotDuplicateFreshDaily — uma ordem carregada CONTA como a
// instance do dia para a def: o RunDaily NÃO cria uma fresca WAITING duplicada.
func TestCarryOver_DoesNotDuplicateFreshDaily(t *testing.T) {
	s := newTestScheduler(t)
	def := defKeep("dup", 0)
	s.mu.Lock()
	s.defs = []domain.JobDefinition{def} // habilitada → RunDaily tentaria criar fresca
	s.mu.Unlock()
	seedInst(t, s, "dup-23", "2026-06-23", string(domain.StatusNotOK), def)

	s.RunDaily("2026-06-24")

	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM instances WHERE order_date=? AND definition_id=?`, "2026-06-24", "dup",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("esperava exatamente 1 instance da def 'dup' em 24 (a carregada), veio %d", n)
	}
	if od, st, _, _ := carriedState(t, s, "dup-23"); od != "2026-06-24" || st != string(domain.StatusNotOK) {
		t.Fatalf("a instance em 24 deveria ser a carregada (NOTOK), veio order=%s status=%s", od, st)
	}
}

// TestCarryOver_Idempotent — rodar a daily duas vezes (auto + botão) não re-move
// nem reconsome orçamento.
func TestCarryOver_Idempotent(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "i-23", "2026-06-23", string(domain.StatusNotOK), defKeep("i", 0))

	s.RunDaily("2026-06-24")
	_, _, _, b1 := carriedState(t, s, "i-23")
	s.RunDaily("2026-06-24") // segunda vez
	od, _, _, b2 := carriedState(t, s, "i-23")
	if od != "2026-06-24" || b1 != 0 || b2 != 0 {
		t.Fatalf("idempotência: esperava order=24 budget=0 estável, veio order=%s b1=%d b2=%d", od, b1, b2)
	}
}

// TestCarryOver_PreservesOriginAcrossDays — carried_from = ORIGEM da ordem,
// estável através de múltiplas viradas (não vira "ontem" a cada dia).
func TestCarryOver_PreservesOriginAcrossDays(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "o-23", "2026-06-23", string(domain.StatusHeld), defKeep("o", 0))

	s.RunDaily("2026-06-24")
	s.RunDaily("2026-06-25")
	if od, _, from, _ := carriedState(t, s, "o-23"); od != "2026-06-25" || from != "2026-06-23" {
		t.Fatalf("origem deveria ficar 2026-06-23 após 2 viradas, veio order=%s from=%s", od, from)
	}
}

// TestWatchdog_CarriedRunningReArmed — um RUNNING carregado com started_at antigo
// mas carried_at recente NÃO é reapado pelo watchdog no instante em que aparece no
// novo dia; com carried_at também antigo, É reapado (agente morto de verdade).
func TestWatchdog_CarriedRunningReArmed(t *testing.T) {
	s := newTestScheduler(t)
	old := time.Now().Add(-30 * time.Minute) // além do stuckRunningTimeout (15min)
	today := time.Now().Format("2006-01-02")
	snap, _ := json.Marshal(defKeep("w", 0))

	// caso A: carried_at recente → re-armado → SEGUE RUNNING.
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at, carried_at, definition_snapshot)
		 VALUES(?,?,?,?,?,?,?,?)`,
		"w-rearmed", "w", today, string(domain.StatusRunning), old, old, time.Now(), string(snap),
	); err != nil {
		t.Fatalf("insert rearmed: %v", err)
	}
	// caso B: carried_at antigo (= started_at) → não re-armado → REAPADO.
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at, carried_at, definition_snapshot)
		 VALUES(?,?,?,?,?,?,?,?)`,
		"w-stale", "w", today, string(domain.StatusRunning), old, old, old, string(snap),
	); err != nil {
		t.Fatalf("insert stale: %v", err)
	}

	s.tickOnce()

	if _, st, _, _ := carriedState(t, s, "w-rearmed"); st != string(domain.StatusRunning) {
		t.Fatalf("RUNNING re-armado não deveria ser reapado, virou %s", st)
	}
	if _, st, _, _ := carriedState(t, s, "w-stale"); st != string(domain.StatusNotOK) {
		t.Fatalf("RUNNING stale (carried_at antigo) deveria ser reapado p/ NOTOK, veio %s", st)
	}
}
