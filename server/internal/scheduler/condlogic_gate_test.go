package scheduler

// CL — o GATE avaliando a lógica booleana de entrada (AND/OR, forma DNF) contra
// o POOL de condições. Complementa o avaliador puro (domain/conditionlogic_test.go):
// aqui a expressão passa pelo gateInstance/Explain com condições reais no pool,
// congelada no definition_snapshot da ordem (imutabilidade M1). Ver
// docs/conditions-events.md §CL.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func todayStr() string { return time.Now().Format("2006-01-02") }

// seedWaitingAt — instance WAITING com order_date e scheduled_at explícitos (p/
// testar o piso de janela contra o token $TIME).
func seedWaitingAt(t *testing.T, s *Scheduler, id, orderDate string, schedAt time.Time, def domain.JobDefinition) {
	t.Helper()
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot) VALUES(?,?,?,?,?,?)`,
		id, def.ID, orderDate, string(domain.StatusWaiting), schedAt, string(snap),
	); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// logicDef — def com lógica de entrada, já normalizada (ConditionsIn ⊇ membros).
func logicDef(id string, logic *domain.ConditionLogic, in []string) domain.JobDefinition {
	d := domain.JobDefinition{
		ID: id, JobType: "COMMAND",
		Schedule:       domain.Schedule{Enabled: true},
		ConditionsIn:   in,
		ConditionLogic: logic,
	}
	return domain.NormalizeConditions([]domain.JobDefinition{d})[0]
}

// staysWaiting — tica algumas vezes e confirma que a instance NÃO saiu de WAITING.
func staysWaiting(t *testing.T, s *Scheduler, id string) {
	t.Helper()
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _ := carriedState(t, s, id); st != string(domain.StatusWaiting) {
		t.Fatalf("%s deveria seguir WAITING, está %s", id, st)
	}
}

func runsFromWaiting(t *testing.T, s *Scheduler, id string) {
	t.Helper()
	st := waitStatus(t, s, id, string(domain.StatusRunning), string(domain.StatusOK))
	if st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("%s deveria ter rodado, está %s", id, st)
	}
}

// (C1 AND C2) OR C3: só roda quando o ramo AND fecha OU quando C3 chega.
func TestCondLogic_Gate_AndOrThird(t *testing.T) {
	s := newTestScheduler(t)
	today := todayStr()
	def := logicDef("J", &domain.ConditionLogic{Op: domain.CondOpOr, Groups: []domain.CondGroup{
		{Op: domain.CondOpAnd, Members: []string{"C1", "C2"}},
		{Op: domain.CondOpAnd, Members: []string{"C3"}},
	}}, []string{"C1", "C2", "C3"})
	seedInst(t, s, "J-1", today, string(domain.StatusWaiting), def)

	// Pool vazio: bloqueado, e o Explain descreve a EXPRESSÃO (não condições soltas).
	staysWaiting(t, s, "J-1")
	ex := explainOf(t, s, "J-1")
	b := hasKind(ex.Blockers, GateCondition)
	if b == nil {
		t.Fatalf("Explain deveria ter WAIT_CONDITION, veio %+v", ex.Blockers)
	}
	if !strings.Contains(b.Detail, "(C1 E C2) OU C3") {
		t.Fatalf("Explain deveria mostrar a expressão, veio %q", b.Detail)
	}

	// C1 só: ramo AND incompleto e C3 ausente → segue bloqueado.
	_ = s.conditions.Set("C1", today, "test")
	staysWaiting(t, s, "J-1")

	// C3 chega: o segundo ramo fecha sozinho → roda.
	_ = s.conditions.Set("C3", today, "test")
	runsFromWaiting(t, s, "J-1")
}

// (C1 OR C2) AND C3: precisa de C3 E de pelo menos um do grupo OR.
func TestCondLogic_Gate_OrAndThird(t *testing.T) {
	s := newTestScheduler(t)
	today := todayStr()
	def := logicDef("K", &domain.ConditionLogic{Op: domain.CondOpAnd, Groups: []domain.CondGroup{
		{Op: domain.CondOpOr, Members: []string{"C1", "C2"}},
		{Op: domain.CondOpAnd, Members: []string{"C3"}},
	}}, []string{"C1", "C2", "C3"})
	seedInst(t, s, "K-1", today, string(domain.StatusWaiting), def)

	// C3 sozinho não basta (grupo OR vazio).
	_ = s.conditions.Set("C3", today, "test")
	staysWaiting(t, s, "K-1")

	// C1 satisfaz o grupo OR → com C3 já no pool, roda.
	_ = s.conditions.Set("C1", today, "test")
	runsFromWaiting(t, s, "K-1")
}

// Imutabilidade M1: a lógica é congelada no definition_snapshot da ordem —
// mudar a lógica na def VIVA não relaxa uma instance já ordenada.
func TestCondLogic_Gate_FrozenInSnapshot(t *testing.T) {
	s := newTestScheduler(t)
	today := todayStr()
	// Ordem congelada com a lógica ESTRITA (C1 AND C2) OR C3.
	strict := logicDef("J", &domain.ConditionLogic{Op: domain.CondOpOr, Groups: []domain.CondGroup{
		{Op: domain.CondOpAnd, Members: []string{"C1", "C2"}},
		{Op: domain.CondOpAnd, Members: []string{"C3"}},
	}}, []string{"C1", "C2", "C3"})
	seedInst(t, s, "J-1", today, string(domain.StatusWaiting), strict)

	// Def VIVA afrouxada para exigir só C1 — se o gate lesse a def viva, C1
	// sozinho destravaria.
	loose := logicDef("J", &domain.ConditionLogic{Op: domain.CondOpAnd, Groups: []domain.CondGroup{
		{Op: domain.CondOpAnd, Members: []string{"C1"}},
	}}, []string{"C1"})
	s.defs = []domain.JobDefinition{loose}

	_ = s.conditions.Set("C1", today, "test")
	staysWaiting(t, s, "J-1") // a lógica congelada ainda exige (C1 E C2) OU C3
}

// CL-2: fallback temporal "(C1) OU ($TIME)". Com $TIME na lógica o PISO de
// janela some (gate 1) — a condição antecipa antes do horário; sem condição o
// horário dispara; e o TETO WindowTo continua fechando.
func TestCondLogic_Gate_TimeFallback(t *testing.T) {
	s := newTestScheduler(t)
	today := todayStr()
	logic := &domain.ConditionLogic{Op: domain.CondOpOr, Groups: []domain.CondGroup{
		{Op: domain.CondOpAnd, Members: []string{"C1"}},
		{Op: domain.CondOpAnd, Members: []string{domain.CondTokenTime}},
	}}
	def := logicDef("T", logic, []string{"C1"})

	// (a) Janela AINDA não abriu (scheduledAt no futuro) e sem C1: bloqueado pela
	// EXPRESSÃO, NÃO pela janela — o piso foi desacoplado (gate 1 pulado).
	future := time.Now().Add(2 * time.Hour)
	seedWaitingAt(t, s, "T-1", today, future, def)
	ex := explainOf(t, s, "T-1")
	if hasKind(ex.Blockers, GateWindow) != nil {
		t.Fatalf("com $TIME na lógica, o gate de JANELA não podia bloquear: %+v", ex.Blockers)
	}
	if hasKind(ex.Blockers, GateCondition) == nil {
		t.Fatalf("sem C1 e antes do horário, deveria bloquear pela expressão: %+v", ex.Blockers)
	}

	// Condição chega ANTES do horário → antecipa (runnable).
	_ = s.conditions.Set("C1", today, "test")
	if ex = explainOf(t, s, "T-1"); !ex.Runnable {
		t.Fatalf("C1 antes do horário deveria antecipar (runnable): %+v", ex.Blockers)
	}

	// (b) Sem C1, mas horário JÁ chegou (scheduledAt no passado) → roda por $TIME.
	past := time.Now().Add(-2 * time.Hour)
	seedWaitingAt(t, s, "T-2", today, past, def)
	if ex = explainOf(t, s, "T-2"); !ex.Runnable {
		t.Fatalf("$TIME atingido deveria rodar sem condição: %+v", ex.Blockers)
	}

	// (c) TETO: WindowTo no passado → WINDOW_CLOSED mesmo com $TIME.
	defTo := logicDef("U", logic, []string{"C1"})
	defTo.Schedule.WindowTo = "00:01"
	seedWaitingAt(t, s, "U-1", today, past, defTo)
	if ex = explainOf(t, s, "U-1"); hasKind(ex.Blockers, GateWindowClosed) == nil {
		t.Fatalf("WindowTo passado deveria fechar (WINDOW_CLOSED) mesmo com $TIME: %+v", ex.Blockers)
	}
}

// Retrocompat: def SEM lógica (nil) segue no caminho clássico — um blocker por
// condição faltante, com o nome da condição (não a expressão).
func TestCondLogic_Gate_NilStaysPerCondBlockers(t *testing.T) {
	s := newTestScheduler(t)
	today := todayStr()
	def := logicDef("L", nil, []string{"C1", "C2"})
	if def.ConditionLogic != nil {
		t.Fatalf("def sem lógica não podia ganhar ConditionLogic, veio %+v", def.ConditionLogic)
	}
	seedInst(t, s, "L-1", today, string(domain.StatusWaiting), def)

	ex := explainOf(t, s, "L-1")
	if b := hasKind(ex.Blockers, GateCondition); b == nil || b.Condition == "" {
		t.Fatalf("caminho clássico deveria emitir blocker por condição com Condition setado, veio %+v", ex.Blockers)
	}
	_ = s.conditions.Set("C1", today, "test")
	_ = s.conditions.Set("C2", today, "test")
	runsFromWaiting(t, s, "L-1")
}
