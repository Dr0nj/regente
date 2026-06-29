package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

const acDate = "2026-06-29"

// schedWithEngines — scheduler de teste com AlertEngine + ConditionEngine
// anexados (os executores das ações dependem deles).
func schedWithEngines(t *testing.T) *Scheduler {
	t.Helper()
	s := newTestScheduler(t)
	s.AttachAlerts(NewAlertEngine(s.db, nil))
	s.AttachConditions(NewConditionEngine(s.db))
	return s
}

// seedAc insere uma instance num status dado, com snapshot da def e attempts.
func seedAc(t *testing.T, s *Scheduler, id string, status domain.InstanceStatus, attempts int, started *time.Time, def domain.JobDefinition) {
	t.Helper()
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at, attempts, definition_snapshot)
		 VALUES(?,?,?,?,?,?,?,?)`,
		id, def.ID, acDate, string(status), time.Now(), started, attempts, string(snap),
	); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func alertCount(t *testing.T, s *Scheduler) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM alert_events`).Scan(&n); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	return n
}

func instStatus(t *testing.T, s *Scheduler, id string) string {
	t.Helper()
	var st string
	if err := s.db.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&st); err != nil {
		t.Fatalf("status %s: %v", id, err)
	}
	return st
}

// TestActionMatches — o DECISOR PURO das três dimensões (sem DB).
func TestActionMatches(t *testing.T) {
	cases := []struct {
		name string
		rule domain.ActionRule
		ev   actionEvent
		want bool
	}{
		{"result NOTOK casa", domain.ActionRule{On: "result", Status: "NOTOK"},
			actionEvent{kind: "result", status: domain.StatusNotOK}, true},
		{"result OK casa", domain.ActionRule{On: "result", Status: "OK"},
			actionEvent{kind: "result", status: domain.StatusOK}, true},
		{"result status divergente não casa", domain.ActionRule{On: "result", Status: "OK"},
			actionEvent{kind: "result", status: domain.StatusNotOK}, false},
		{"dimensão divergente não casa", domain.ActionRule{On: "result", Status: "NOTOK"},
			actionEvent{kind: "attempt", attempt: 1}, false},
		{"attempt exato casa", domain.ActionRule{On: "attempt", Attempt: 2},
			actionEvent{kind: "attempt", attempt: 2}, true},
		{"attempt diferente não casa", domain.ActionRule{On: "attempt", Attempt: 2},
			actionEvent{kind: "attempt", attempt: 1}, false},
		{"attempt zero (não configurado) nunca casa", domain.ActionRule{On: "attempt", Attempt: 0},
			actionEvent{kind: "attempt", attempt: 0}, false},
		{"runtime acima do limiar casa", domain.ActionRule{On: "runtime", AfterMin: 30},
			actionEvent{kind: "runtime", runMin: 31}, true},
		{"runtime no limiar casa (>=)", domain.ActionRule{On: "runtime", AfterMin: 30},
			actionEvent{kind: "runtime", runMin: 30}, true},
		{"runtime abaixo não casa", domain.ActionRule{On: "runtime", AfterMin: 30},
			actionEvent{kind: "runtime", runMin: 29}, false},
		{"case-insensitive On/Status", domain.ActionRule{On: "Result", Status: "notok"},
			actionEvent{kind: "result", status: domain.StatusNotOK}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := actionMatches(c.rule, c.ev); got != c.want {
				t.Fatalf("actionMatches=%v, want %v", got, c.want)
			}
		})
	}
}

// TestClaimActionFire — idempotência: claim só na primeira vez.
func TestClaimActionFire(t *testing.T) {
	s := newTestScheduler(t)
	if !s.claimActionFire("i1", "0") {
		t.Fatal("1º claim deveria ser true")
	}
	if s.claimActionFire("i1", "0") {
		t.Fatal("2º claim da mesma chave deveria ser false")
	}
	if !s.claimActionFire("i1", "1") {
		t.Fatal("chave diferente deveria reivindicar")
	}
	if !s.claimActionFire("i2", "0") {
		t.Fatal("instance diferente deveria reivindicar")
	}
}

// TestAction_ResultNotify — On result NOTOK → notify dispara um alerta, uma vez.
func TestAction_ResultNotify(t *testing.T) {
	s := schedWithEngines(t)
	def := domain.JobDefinition{ID: "j", Label: "Job J", JobType: "COMMAND", Actions: []domain.ActionRule{
		{On: "result", Status: "NOTOK", Do: "notify", Message: "deu ruim", Severity: "critical"},
	}}
	now := time.Now()
	seedAc(t, s, "j-1", domain.StatusRunning, 1, &now, def)

	s.FinishInstance("j-1", domain.StatusNotOK, 1, "boom")
	if got := alertCount(t, s); got != 1 {
		t.Fatalf("esperava 1 alerta após notify, veio %d", got)
	}
	// Re-finish (idempotência): não dispara de novo.
	s.FinishInstance("j-1", domain.StatusNotOK, 1, "boom")
	if got := alertCount(t, s); got != 1 {
		t.Fatalf("notify deveria ser idempotente (1), veio %d", got)
	}
}

// TestAction_ResultOK_NoFireForNotokRule — regra On NOTOK não dispara em OK.
func TestAction_ResultOK_NoFireForNotokRule(t *testing.T) {
	s := schedWithEngines(t)
	def := domain.JobDefinition{ID: "j", Label: "Job J", JobType: "COMMAND", Actions: []domain.ActionRule{
		{On: "result", Status: "NOTOK", Do: "notify"},
	}}
	now := time.Now()
	seedAc(t, s, "j-ok", domain.StatusRunning, 1, &now, def)
	s.FinishInstance("j-ok", domain.StatusOK, 0, "")
	if got := alertCount(t, s); got != 0 {
		t.Fatalf("regra NOTOK não deveria disparar em OK, veio %d alertas", got)
	}
}

// TestAction_AttemptFinal — On attempt N → dispara na tentativa N que falhou
// (testa a tentativa final sem depender do timer do retry).
func TestAction_AttemptFinal(t *testing.T) {
	s := schedWithEngines(t)
	// Retries=1 → maxAttempts=2; seeda attempts=2 (final) → maybeRetry não re-roda.
	def := domain.JobDefinition{ID: "j", Label: "Job J", JobType: "COMMAND", Retries: 1, Actions: []domain.ActionRule{
		{On: "attempt", Attempt: 2, Do: "notify", Message: "2ª tentativa falhou"},
	}}
	now := time.Now()
	seedAc(t, s, "j-att", domain.StatusRunning, 2, &now, def)
	s.FinishInstance("j-att", domain.StatusNotOK, 1, "boom")
	if got := alertCount(t, s); got != 1 {
		t.Fatalf("esperava 1 alerta da tentativa 2, veio %d", got)
	}
}

// TestAction_Runtime — On runtime>30min → dispara no tick para RUNNING antigo.
func TestAction_Runtime(t *testing.T) {
	s := schedWithEngines(t)
	def := domain.JobDefinition{ID: "j", Label: "Job J", JobType: "COMMAND", Actions: []domain.ActionRule{
		{On: "runtime", AfterMin: 30, Do: "notify", Message: "rodando demais", Severity: "warning"},
	}}
	old := time.Now().Add(-40 * time.Minute)
	seedAc(t, s, "j-run", domain.StatusRunning, 1, &old, def)

	s.evaluateRuntimeActions(time.Now())
	if got := alertCount(t, s); got != 1 {
		t.Fatalf("esperava 1 shout de runtime, veio %d", got)
	}
	// Tick de novo: idempotente (limiar já disparou).
	s.evaluateRuntimeActions(time.Now())
	if got := alertCount(t, s); got != 1 {
		t.Fatalf("shout de runtime deveria ser idempotente (1), veio %d", got)
	}
}

// TestAction_Runtime_BelowThreshold — RUNNING recente não dispara.
func TestAction_Runtime_BelowThreshold(t *testing.T) {
	s := schedWithEngines(t)
	def := domain.JobDefinition{ID: "j", JobType: "COMMAND", Actions: []domain.ActionRule{
		{On: "runtime", AfterMin: 30, Do: "notify"},
	}}
	recent := time.Now().Add(-5 * time.Minute)
	seedAc(t, s, "j-young", domain.StatusRunning, 1, &recent, def)
	s.evaluateRuntimeActions(time.Now())
	if got := alertCount(t, s); got != 0 {
		t.Fatalf("RUNNING de 5min não deveria disparar shout de 30min, veio %d", got)
	}
}

// TestAction_SetCondition — On result OK → set-condition cria a condition no escopo.
func TestAction_SetCondition(t *testing.T) {
	s := schedWithEngines(t)
	def := domain.JobDefinition{ID: "j", JobType: "COMMAND", Actions: []domain.ActionRule{
		{On: "result", Status: "OK", Do: "set-condition", Condition: "BILLING_DONE"},
	}}
	now := time.Now()
	seedAc(t, s, "j-cond", domain.StatusRunning, 1, &now, def)
	s.FinishInstance("j-cond", domain.StatusOK, 0, "")
	if !s.conditions.Has("BILLING_DONE", acDate) {
		t.Fatal("set-condition deveria ter criado BILLING_DONE no escopo do order_date")
	}
}

// TestAction_RunJob — On result NOTOK → run-job força a ordem do job alvo.
func TestAction_RunJob(t *testing.T) {
	s := schedWithEngines(t)
	target := domain.JobDefinition{ID: "cleanup", Label: "Cleanup", JobType: "COMMAND"}
	s.defs = []domain.JobDefinition{target} // ForceOrder lê de s.defs
	def := domain.JobDefinition{ID: "j", JobType: "COMMAND", Actions: []domain.ActionRule{
		{On: "result", Status: "NOTOK", Do: "run-job", TargetJob: "cleanup"},
	}}
	now := time.Now()
	seedAc(t, s, "j-rj", domain.StatusRunning, 1, &now, def)
	s.FinishInstance("j-rj", domain.StatusNotOK, 1, "boom")

	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM instances WHERE definition_id='cleanup' AND forced=1`,
	).Scan(&n); err != nil {
		t.Fatalf("query forced: %v", err)
	}
	if n != 1 {
		t.Fatalf("esperava 1 instance forçada de cleanup, veio %d", n)
	}
}

// TestAction_SetOK — On result NOTOK → set-ok faz auto-heal (NOTOK→OK).
func TestAction_SetOK(t *testing.T) {
	s := schedWithEngines(t)
	def := domain.JobDefinition{ID: "j", JobType: "COMMAND", Actions: []domain.ActionRule{
		{On: "result", Status: "NOTOK", Do: "set-ok"},
	}}
	now := time.Now()
	seedAc(t, s, "j-heal", domain.StatusRunning, 1, &now, def)
	s.FinishInstance("j-heal", domain.StatusNotOK, 1, "boom")
	if st := instStatus(t, s, "j-heal"); st != string(domain.StatusOK) {
		t.Fatalf("set-ok deveria ter flipado para OK, status=%s", st)
	}
}

// TestAction_MultipleRulesSameTrigger — duas regras no mesmo gatilho disparam
// AMBAS (chave por índice, não por gatilho).
func TestAction_MultipleRulesSameTrigger(t *testing.T) {
	s := schedWithEngines(t)
	def := domain.JobDefinition{ID: "j", JobType: "COMMAND", Actions: []domain.ActionRule{
		{On: "result", Status: "NOTOK", Do: "notify", Message: "regra A"},
		{On: "result", Status: "NOTOK", Do: "notify", Message: "regra B"},
	}}
	now := time.Now()
	seedAc(t, s, "j-multi", domain.StatusRunning, 1, &now, def)
	s.FinishInstance("j-multi", domain.StatusNotOK, 1, "boom")
	if got := alertCount(t, s); got != 2 {
		t.Fatalf("duas regras no mesmo gatilho deveriam disparar 2 alertas, veio %d", got)
	}
}
