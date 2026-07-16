package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// ---------------------------------------------------------------------------
// Ciclo de vida da daily (carry-over entre diárias, Control-M New Day).
//
// Desde 2026-07-16 as idades são em DIAS-CALENDÁRIO a partir do ODAT (origem)
// ou da última atividade — não em "número de viradas". Um New Day que não
// rodou (server desligado) NÃO estica a vida de ninguém: job do dia 14 com
// keepActive=0 não pode aparecer no dia 16 (report do usuário).
// ---------------------------------------------------------------------------

// def helper com keepActive embutido no schedule.
func defKeep(id string, keepActive int) domain.JobDefinition {
	return domain.JobDefinition{
		ID: id, JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true, KeepActive: keepActive},
	}
}

// seedInst insere uma instance num order_date com snapshot da def. Retorna o id.
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

// seedInstRun — como seedInst, mas com attempts e timestamps de execução
// (started/finished no MEIO-DIA da data dada, pra idade em dias ser estável).
func seedInstRun(t *testing.T, s *Scheduler, id, date, status string, def domain.JobDefinition, attempts int, startedDay, finishedDay string) string {
	t.Helper()
	snap, _ := json.Marshal(def)
	noonOf := func(d string) any {
		if d == "" {
			return nil
		}
		tt, err := time.ParseInLocation("2006-01-02", d, time.Local)
		if err != nil {
			t.Fatalf("data %q: %v", d, err)
		}
		return tt.Add(12 * time.Hour)
	}
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot, attempts, started_at, finished_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		id, def.ID, date, status, time.Now(), string(snap), attempts, noonOf(startedDay), noonOf(finishedDay),
	); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	return id
}

// carriedState lê o estado de carry de uma instance.
func carriedState(t *testing.T, s *Scheduler, id string) (orderDate, status, from string) {
	t.Helper()
	err := s.db.QueryRow(
		`SELECT order_date, status, COALESCE(carried_from,'') FROM instances WHERE id=?`, id,
	).Scan(&orderDate, &status, &from)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return
}

// TestCarryDecision — a REGRA pura, sem banco. Idades em dias-calendário.
func TestCarryDecision(t *testing.T) {
	plain := defKeep("a", 0)
	keep3 := defKeep("a", 3)

	cases := []struct {
		name         string
		status       string
		retryPending bool
		ageDays      int // hoje − ODAT
		actDays      int // hoje − última atividade (falha/execução)
		def          domain.JobDefinition
		wantCarry    bool
	}{
		{"running sempre carrega", string(domain.StatusRunning), false, 5, 5, plain, true},
		{"held sempre carrega", string(domain.StatusHeld), false, 9, 9, plain, true},
		{"notok default atravessa 1 diária após a falha", string(domain.StatusNotOK), false, 1, 1, plain, true},
		{"notok default morre na 2ª diária após a falha", string(domain.StatusNotOK), false, 2, 2, plain, false},
		{"notok que falhou HOJE após atravessar dias RUNNING ganha +1", string(domain.StatusNotOK), false, 3, 1, plain, true},
		{"notok keepActive=3 vive 3 diárias", string(domain.StatusNotOK), false, 3, 3, keep3, true},
		{"notok keepActive=3 morre na 4ª", string(domain.StatusNotOK), false, 4, 4, keep3, false},
		// REGRA do usuário (2026-07-16): WAITING obedece keepActive ESTRITO —
		// keepActive=0 morre na primeira virada. Vale também pra job aguardando
		// Confirm (é WAITING no banco).
		{"waiting keepActive=0 não carrega", string(domain.StatusWaiting), false, 1, 1, plain, false},
		{"waiting keepActive=3 carrega até 3 diárias", string(domain.StatusWaiting), false, 3, 3, keep3, true},
		{"waiting keepActive=3 morre na 4ª", string(domain.StatusWaiting), false, 4, 4, keep3, false},
		// Gap de New Day (server desligado): a idade é em DIAS, não viradas —
		// 14→16 em uma virada só conta 2 dias e mata keepActive=0/1.
		{"waiting keepActive=0 com gap 2 dias não carrega", string(domain.StatusWaiting), false, 2, 2, plain, false},
		{"notok com gap 2 dias não carrega", string(domain.StatusNotOK), false, 2, 2, plain, false},
		// D-1 — retry AGENDADO em andamento: NOTOK em tratamento (baseline 1).
		{"retry-pendente carrega como notok", string(domain.StatusWaiting), true, 1, 1, plain, true},
		{"retry-pendente expira como notok", string(domain.StatusWaiting), true, 2, 2, plain, false},
		{"retry-pendente keepActive=3 vive 3 diárias", string(domain.StatusWaiting), true, 3, 3, keep3, true},
		{"ok nunca carrega", string(domain.StatusOK), false, 1, 1, plain, false},
		{"cancelled nunca carrega", string(domain.StatusCancelled), false, 1, 1, plain, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := carryDecision(c.status, c.retryPending, c.ageDays, c.actDays, c.def)
			if got.carry != c.wantCarry {
				t.Fatalf("carry=%v, esperava %v (reason=%s)", got.carry, c.wantCarry, got.reason)
			}
		})
	}
}

// TestCarryOver_RunningPersists — REGRA: um job RUNNING na virada NÃO some; segue
// na daily de hoje (mesmo id/status), com carried_from = ODAT de origem.
func TestCarryOver_RunningPersists(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "r-23", "2026-06-23", string(domain.StatusRunning), defKeep("r", 0))

	if created := s.RunDaily("2026-06-24"); created != 0 {
		t.Fatalf("sem defs habilitadas, RunDaily deveria criar 0 frescas, criou %d", created)
	}
	od, st, from := carriedState(t, s, "r-23")
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
	if od, st, _ := carriedState(t, s, "h-23"); od != "2026-06-24" || st != string(domain.StatusHeld) {
		t.Fatalf("HELD deveria persistir em 24, veio order=%s status=%s", od, st)
	}
	// Continua em hold → atravessa a próxima virada também.
	s.RunDaily("2026-06-25")
	if od, _, _ := carriedState(t, s, "h-23"); od != "2026-06-25" {
		t.Fatalf("HELD deveria seguir pra 25, ficou em %s", od)
	}
}

// TestCarryOver_NotOKDefaultOneDay — DEFAULT: NOTOK não-tratado persiste +1 diária
// após a FALHA, depois para.
func TestCarryOver_NotOKDefaultOneDay(t *testing.T) {
	s := newTestScheduler(t)
	seedInstRun(t, s, "x-23", "2026-06-23", string(domain.StatusNotOK), defKeep("x", 0), 1, "2026-06-23", "2026-06-23")

	s.RunDaily("2026-06-24")
	if od, _, _ := carriedState(t, s, "x-23"); od != "2026-06-24" {
		t.Fatalf("NOTOK default deveria ir pra 24, veio order=%s", od)
	}
	// Segunda virada: 2 dias desde a falha → NÃO carrega.
	s.RunDaily("2026-06-25")
	if od, _, _ := carriedState(t, s, "x-23"); od != "2026-06-24" {
		t.Fatalf("NOTOK esgotado NÃO deveria ir pra 25, foi pra %s", od)
	}
}

// TestCarryOver_KeepActiveSurvivesNDays — keepActive=2 sobrevive 2 diárias extras.
func TestCarryOver_KeepActiveSurvivesNDays(t *testing.T) {
	s := newTestScheduler(t)
	seedInstRun(t, s, "y-23", "2026-06-23", string(domain.StatusNotOK), defKeep("y", 2), 1, "2026-06-23", "2026-06-23")

	s.RunDaily("2026-06-24") // 1 dia desde a falha ≤ 2
	if od, _, _ := carriedState(t, s, "y-23"); od != "2026-06-24" {
		t.Fatalf("dia 24: esperava order=24, veio order=%s", od)
	}
	s.RunDaily("2026-06-25") // 2 dias ≤ 2
	if od, _, _ := carriedState(t, s, "y-23"); od != "2026-06-25" {
		t.Fatalf("dia 25: esperava order=25, veio order=%s", od)
	}
	s.RunDaily("2026-06-26") // 3 dias > 2 → para
	if od, _, _ := carriedState(t, s, "y-23"); od != "2026-06-25" {
		t.Fatalf("dia 26: keepActive=2 esgotado, NÃO deveria ir além de 25, foi pra %s", od)
	}
}

// TestCarryOver_GapCountsCalendarDays — o report do usuário (2026-07-16): o
// server ficou desligado no dia 15 e a virada seguinte foi direto 14→16. A
// idade conta DIAS-CALENDÁRIO: WAITING keepActive=0 e NOTOK (baseline 1 após a
// falha do dia 14) NÃO aparecem no dia 16; RUNNING/HELD atravessam sempre;
// keepActive=2 cobre o gap.
func TestCarryOver_GapCountsCalendarDays(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "w-14", "2026-07-14", string(domain.StatusWaiting), defKeep("w", 0))
	seedInstRun(t, s, "n-14", "2026-07-14", string(domain.StatusNotOK), defKeep("n", 0), 1, "2026-07-14", "2026-07-14")
	seedInst(t, s, "k-14", "2026-07-14", string(domain.StatusWaiting), defKeep("k", 2))
	seedInst(t, s, "r-14", "2026-07-14", string(domain.StatusRunning), defKeep("r", 0))
	seedInst(t, s, "h-14", "2026-07-14", string(domain.StatusHeld), defKeep("h", 0))

	s.RunDaily("2026-07-16") // a diária de 15 nunca rodou

	if od, _, _ := carriedState(t, s, "w-14"); od != "2026-07-14" {
		t.Fatalf("WAITING keepActive=0 do dia 14 NÃO pode aparecer no 16, foi pra %s", od)
	}
	if od, _, _ := carriedState(t, s, "n-14"); od != "2026-07-14" {
		t.Fatalf("NOTOK do dia 14 (falha há 2 dias) NÃO pode aparecer no 16, foi pra %s", od)
	}
	if od, _, _ := carriedState(t, s, "k-14"); od != "2026-07-16" {
		t.Fatalf("WAITING keepActive=2 deveria cobrir o gap até o 16, ficou em %s", od)
	}
	if od, _, _ := carriedState(t, s, "r-14"); od != "2026-07-16" {
		t.Fatalf("RUNNING atravessa sempre, ficou em %s", od)
	}
	if od, _, _ := carriedState(t, s, "h-14"); od != "2026-07-16" {
		t.Fatalf("HELD atravessa sempre, ficou em %s", od)
	}
}

// TestCarryOver_OperatorRerunIsNotRetryPending — rerun de OPERADOR zera
// started_at (ver rerunInstance): um WAITING re-rodado com attempts>1 NÃO é um
// "retry em tratamento" — obedece keepActive estrito. Já o retry AGENDADO do
// scheduler (D-1) mantém started_at e carrega com a regra do NOTOK.
func TestCarryOver_OperatorRerunIsNotRetryPending(t *testing.T) {
	s := newTestScheduler(t)
	// rerun de operador: attempts=2, started_at NULL (o handler zera).
	seedInstRun(t, s, "op-23", "2026-06-23", string(domain.StatusWaiting), defKeep("op", 0), 2, "", "")
	// retry agendado (D-1): attempts=2, started_at preenchido.
	seedInstRun(t, s, "rt-23", "2026-06-23", string(domain.StatusWaiting), defKeep("rt", 0), 2, "2026-06-23", "")

	s.RunDaily("2026-06-24")

	if od, _, _ := carriedState(t, s, "op-23"); od != "2026-06-23" {
		t.Fatalf("rerun de operador com keepActive=0 NÃO deveria carregar, foi pra %s", od)
	}
	if od, _, _ := carriedState(t, s, "rt-23"); od != "2026-06-24" {
		t.Fatalf("retry agendado deveria carregar (+1 após a execução), ficou em %s", od)
	}
}

// TestCarryOver_OKAndCancelledDoNotCarry — encerrados não atravessam a virada.
func TestCarryOver_OKAndCancelledDoNotCarry(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "ok-23", "2026-06-23", string(domain.StatusOK), defKeep("ok", 5))
	seedInst(t, s, "cx-23", "2026-06-23", string(domain.StatusCancelled), defKeep("cx", 5))

	s.RunDaily("2026-06-24")
	if od, _, _ := carriedState(t, s, "ok-23"); od != "2026-06-23" {
		t.Fatalf("OK não deveria carregar, foi pra %s", od)
	}
	if od, _, _ := carriedState(t, s, "cx-23"); od != "2026-06-23" {
		t.Fatalf("CANCELLED não deveria carregar, foi pra %s", od)
	}
}

// TestCarryOver_CarriedDoesNotBlockFreshDaily — BUG-12: uma ordem CARREGADA
// (carry-over) NÃO conta como a instance do dia — o RunDaily materializa a
// fresca de hoje AO LADO da carregada (New Day do Control-M: o NOTOK de ontem
// fica em tratamento e o job de hoje entra normal). Re-rodar a daily segue
// idempotente: a fresca (carried_from='') é quem bloqueia duplicata.
func TestCarryOver_CarriedDoesNotBlockFreshDaily(t *testing.T) {
	s := newTestScheduler(t)
	def := defKeep("dup", 0)
	s.mu.Lock()
	s.defs = []domain.JobDefinition{def} // habilitada → RunDaily cria a fresca
	s.mu.Unlock()
	seedInstRun(t, s, "dup-23", "2026-06-23", string(domain.StatusNotOK), def, 1, "2026-06-23", "2026-06-23")

	s.RunDaily("2026-06-24")

	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM instances WHERE order_date=? AND definition_id=?`, "2026-06-24", "dup",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("esperava 2 instances da def 'dup' em 24 (carregada + fresca), veio %d", n)
	}
	if od, st, _ := carriedState(t, s, "dup-23"); od != "2026-06-24" || st != string(domain.StatusNotOK) {
		t.Fatalf("a carregada deveria seguir NOTOK em 24, veio order=%s status=%s", od, st)
	}
	var freshStatus string
	if err := s.db.QueryRow(
		`SELECT status FROM instances WHERE id=?`, "dup-2026-06-24",
	).Scan(&freshStatus); err != nil || freshStatus != string(domain.StatusWaiting) {
		t.Fatalf("a fresca dup-2026-06-24 deveria existir WAITING, veio status=%s err=%v", freshStatus, err)
	}

	// Idempotência: re-rodar a daily não cria uma terceira.
	s.RunDaily("2026-06-24")
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM instances WHERE order_date=? AND definition_id=?`, "2026-06-24", "dup",
	).Scan(&n); err != nil || n != 2 {
		t.Fatalf("re-rodar a daily deveria manter 2 instances, veio %d (err=%v)", n, err)
	}
}

// TestCarryOver_Idempotent — rodar a daily duas vezes (auto + botão) não re-move.
func TestCarryOver_Idempotent(t *testing.T) {
	s := newTestScheduler(t)
	seedInstRun(t, s, "i-23", "2026-06-23", string(domain.StatusNotOK), defKeep("i", 0), 1, "2026-06-23", "2026-06-23")

	s.RunDaily("2026-06-24")
	od1, _, from1 := carriedState(t, s, "i-23")
	s.RunDaily("2026-06-24") // segunda vez
	od2, _, from2 := carriedState(t, s, "i-23")
	if od1 != "2026-06-24" || od2 != "2026-06-24" || from1 != "2026-06-23" || from2 != "2026-06-23" {
		t.Fatalf("idempotência: esperava order=24 from=23 estável, veio od1=%s od2=%s from1=%s from2=%s", od1, od2, from1, from2)
	}
}

// TestCarryOver_PreservesOriginAcrossDays — carried_from = ODAT (origem da
// ordem), estável através de múltiplas viradas (não vira "ontem" a cada dia).
func TestCarryOver_PreservesOriginAcrossDays(t *testing.T) {
	s := newTestScheduler(t)
	seedInst(t, s, "o-23", "2026-06-23", string(domain.StatusHeld), defKeep("o", 0))

	s.RunDaily("2026-06-24")
	s.RunDaily("2026-06-25")
	if od, _, from := carriedState(t, s, "o-23"); od != "2026-06-25" || from != "2026-06-23" {
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

	if _, st, _ := carriedState(t, s, "w-rearmed"); st != string(domain.StatusRunning) {
		t.Fatalf("RUNNING re-armado não deveria ser reapado, virou %s", st)
	}
	if _, st, _ := carriedState(t, s, "w-stale"); st != string(domain.StatusNotOK) {
		t.Fatalf("RUNNING stale (carried_at antigo) deveria ser reapado p/ NOTOK, veio %s", st)
	}
}

// ---------------------------------------------------------------------------
// ODAT — escopo de data de eventos e conditions (2026-07-16).
// ---------------------------------------------------------------------------

// TestOdat_CarriedConsumerDoesNotClaimFreshEvent — o report do usuário: um
// consumidor CARREGADO do dia 14 não pode latchar o evento do pai FRESCO de
// hoje (nem o contrário). O evento é emitido com o ODAT do produtor e o claim
// casa evento×consumidor pela MESMA origem.
func TestOdat_CarriedConsumerDoesNotClaimFreshEvent(t *testing.T) {
	s := newTestScheduler(t)
	parent := defKeep("pai", 0)
	child := domain.JobDefinition{
		ID: "filho", JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "pai", Condition: domain.CondOnSuccess}},
	}
	today := time.Now().Format("2006-01-02")

	// Consumidor carregado do dia 14 (order_date avançado, ODAT preservado).
	snapC, _ := json.Marshal(child)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot, carried_from)
		 VALUES(?,?,?,?,?,?,?)`,
		"filho-14", "filho", today, string(domain.StatusWaiting), time.Now(), string(snapC), "2026-07-14",
	); err != nil {
		t.Fatalf("seed filho-14: %v", err)
	}
	// Pai FRESCO de hoje termina OK → evento com ODAT de hoje.
	seedInst(t, s, "pai-hoje", today, string(domain.StatusRunning), parent)
	s.FinishInstance("pai-hoje", domain.StatusOK, 0, "")

	// O carregado tenta latchar: NÃO pode (datas de origem diferentes).
	r := instRow{ID: "filho-14", DefID: "filho", OrderDate: today, CarriedFrom: "2026-07-14", Status: string(domain.StatusWaiting)}
	if s.claimDepEdges(r, child, map[string]bool{}) {
		t.Fatalf("consumidor do dia 14 latchou o evento do pai fresco de hoje — escopo ODAT violado")
	}

	// Pai carregado da MESMA origem termina → o carregado latcha.
	snapP, _ := json.Marshal(parent)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot, carried_from)
		 VALUES(?,?,?,?,?,?,?)`,
		"pai-14", "pai", today, string(domain.StatusRunning), time.Now(), string(snapP), "2026-07-14",
	); err != nil {
		t.Fatalf("seed pai-14: %v", err)
	}
	s.FinishInstance("pai-14", domain.StatusOK, 0, "")
	if !s.claimDepEdges(r, child, map[string]bool{}) {
		t.Fatalf("consumidor do dia 14 deveria latchar o término do pai da MESMA origem")
	}

	// E o evento consumido pelo carregado não sobra pro fresco de hoje... o
	// fresco espera o pai de HOJE (que já emitiu) — latcha o evento de hoje.
	seedInst(t, s, "filho-hoje", today, string(domain.StatusWaiting), child)
	rf := instRow{ID: "filho-hoje", DefID: "filho", OrderDate: today, Status: string(domain.StatusWaiting)}
	if !s.claimDepEdges(rf, child, map[string]bool{}) {
		t.Fatalf("consumidor fresco deveria latchar o evento do pai fresco (mesma origem: hoje)")
	}
}

// TestOdat_DateRefPrevAndStat — dateRef=prev latcha o término do pai da diária
// ANTERIOR; dateRef=stat latcha qualquer término livre, sem olhar data.
func TestOdat_DateRefPrevAndStat(t *testing.T) {
	s := newTestScheduler(t)
	parent := defKeep("pai", 0)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	// Pai de ONTEM terminou OK (evento com ODAT de ontem, ainda livre).
	seedInst(t, s, "pai-ontem", yesterday, string(domain.StatusRunning), parent)
	s.FinishInstance("pai-ontem", domain.StatusOK, 0, "")

	childPrev := domain.JobDefinition{
		ID: "filho-prev", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "pai", Condition: domain.CondOnSuccess, DateRef: domain.DateRefPrev}},
	}
	seedInst(t, s, "fp-hoje", today, string(domain.StatusWaiting), childPrev)
	rp := instRow{ID: "fp-hoje", DefID: "filho-prev", OrderDate: today, Status: string(domain.StatusWaiting)}
	if !s.claimDepEdges(rp, childPrev, map[string]bool{}) {
		t.Fatalf("dateRef=prev deveria latchar o término do pai de ontem")
	}

	// stat: pai de anteontem, qualquer data serve.
	older := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	seedInst(t, s, "pai-velho", older, string(domain.StatusRunning), parent)
	s.FinishInstance("pai-velho", domain.StatusOK, 0, "")
	childStat := domain.JobDefinition{
		ID: "filho-stat", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "pai", Condition: domain.CondOnSuccess, DateRef: domain.DateRefStat}},
	}
	seedInst(t, s, "fs-hoje", today, string(domain.StatusWaiting), childStat)
	rs := instRow{ID: "fs-hoje", DefID: "filho-stat", OrderDate: today, Status: string(domain.StatusWaiting)}
	if !s.claimDepEdges(rs, childStat, map[string]bool{}) {
		t.Fatalf("dateRef=stat deveria latchar qualquer término livre do pai")
	}
}

// TestOdat_ConditionSuffixes — conditions F16 com @odat (default), @prev e
// @stat: o gate resolve o escopo contra o ODAT do job; ConditionsOut cria no
// ODAT do produtor (não no order_date avançado).
func TestOdat_ConditionSuffixes(t *testing.T) {
	s := newTestScheduler(t)
	if s.conditions == nil {
		s.AttachConditions(NewConditionEngine(s.db))
	}
	c := s.conditions
	prevOf := s.prevDaily

	// default (@odat): existe só se setada na data de origem.
	_ = c.Set("A", "2026-07-14", "test")
	if miss := c.Missing([]string{"A"}, "2026-07-14", prevOf); len(miss) != 0 {
		t.Fatalf("A@odat deveria estar satisfeita em 14, faltou %v", miss)
	}
	if miss := c.Missing([]string{"A"}, "2026-07-16", prevOf); len(miss) != 1 {
		t.Fatalf("A do dia 14 NÃO pode satisfazer o job do dia 16, veio %v", miss)
	}

	// @stat: só a permanente satisfaz.
	if miss := c.Missing([]string{"B@stat"}, "2026-07-14", prevOf); len(miss) != 1 {
		t.Fatalf("B@stat sem permanente deveria faltar")
	}
	_ = c.Set("B", "", "test")
	if miss := c.Missing([]string{"B@stat"}, "2026-07-14", prevOf); len(miss) != 0 {
		t.Fatalf("B@stat com permanente deveria satisfazer")
	}

	// @prev: resolve pra diária anterior (sem daily_runs, ODAT-1).
	_ = c.Set("C", "2026-07-15", "test")
	if miss := c.Missing([]string{"C@prev"}, "2026-07-16", prevOf); len(miss) != 0 {
		t.Fatalf("C@prev do dia 16 deveria achar a condition do dia 15, faltou %v", miss)
	}

	// ConditionsOut no ODAT do produtor: instance carregada do dia 14 cria a
	// condition DO DIA 14.
	def := domain.JobDefinition{
		ID: "prod", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
		ConditionsOutAdd: []string{"FEITO", "GLOBAL@stat"},
	}
	today := time.Now().Format("2006-01-02")
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot, carried_from)
		 VALUES(?,?,?,?,?,?,?)`,
		"prod-14", "prod", today, string(domain.StatusRunning), time.Now(), string(snap), "2026-07-14",
	); err != nil {
		t.Fatalf("seed prod-14: %v", err)
	}
	s.mu.Lock()
	s.defs = []domain.JobDefinition{def}
	s.mu.Unlock()
	s.applyConditionsOut("prod-14", "test")
	if !c.Has("FEITO", "2026-07-14") {
		t.Fatalf("FEITO deveria existir no ODAT 2026-07-14 do produtor carregado")
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM conditions WHERE name='FEITO' AND scope_date=?`, today).Scan(&n)
	if n != 0 {
		t.Fatalf("FEITO NÃO deveria existir no order_date avançado (%s)", today)
	}
	if !c.Has("GLOBAL", "") {
		t.Fatalf("GLOBAL@stat deveria existir como permanente")
	}
}
