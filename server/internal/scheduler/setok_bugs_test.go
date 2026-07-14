package scheduler

// Regressão BUG-3/BUG-4 (report 2026-07-13): Set OK vale para WAITING (WAIT
// EVENT) e MATERIALIZA as saídas do job como um término OK de verdade —
// dep_event consumível (schemaV15) + ConditionsOut (F16).

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// Set OK num job WAITING (preso em WAIT EVENT) conclui OK, publica o evento
// consumível e emite as ConditionsOut da def.
func TestSetOK_FromWaitingMaterializesOutputs(t *testing.T) {
	s := newTestScheduler(t)
	s.AttachConditions(NewConditionEngine(s.db))
	today := time.Now().Format("2006-01-02")

	def := domain.JobDefinition{
		ID: "prod", JobType: "COMMAND",
		Schedule:         domain.Schedule{Enabled: true},
		ConditionsOutAdd: []string{"prod-done"},
	}
	s.defs = []domain.JobDefinition{def} // applyConditionsOut lê a def viva
	seedInst(t, s, "prod-1", today, string(domain.StatusWaiting), def)

	if err := s.SetOK("prod-1"); err != nil {
		t.Fatalf("SetOK em WAITING deveria valer (BUG-3), veio: %v", err)
	}

	if _, st, _, _ := carriedState(t, s, "prod-1"); st != string(domain.StatusOK) {
		t.Fatalf("esperava OK, veio %s", st)
	}
	var events int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM dep_events WHERE instance_id=?`, "prod-1").Scan(&events); err != nil || events != 1 {
		t.Fatalf("Set OK deveria emitir 1 dep_event, veio %d (err=%v)", events, err)
	}
	// BUG-4: a ConditionOut materializa — dependentes por condition destravam.
	if !s.conditions.Has("prod-done", today) {
		t.Fatal("Set OK deveria materializar a ConditionOut 'prod-done' (BUG-4)")
	}
}

// Set OK a partir de NOTOK segue emitindo as ConditionsOut (pai falho tratado
// pelo operador destrava dependentes por condition, não só por dep-event).
func TestSetOK_FromNotOKAppliesConditionsOut(t *testing.T) {
	s := newTestScheduler(t)
	s.AttachConditions(NewConditionEngine(s.db))
	today := time.Now().Format("2006-01-02")

	def := domain.JobDefinition{
		ID: "flip", JobType: "COMMAND",
		Schedule:         domain.Schedule{Enabled: true},
		ConditionsOutAdd: []string{"flip-done"},
	}
	s.defs = []domain.JobDefinition{def}
	seedInst(t, s, "flip-1", today, string(domain.StatusNotOK), def)

	if err := s.SetOK("flip-1"); err != nil {
		t.Fatalf("SetOK: %v", err)
	}
	if !s.conditions.Has("flip-done", today) {
		t.Fatal("Set OK de NOTOK deveria materializar a ConditionOut 'flip-done' (BUG-4)")
	}
}

// Estados de execução/terminais OK seguem rejeitados.
func TestSetOK_InvalidStates(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "x", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}

	seedInst(t, s, "x-run", today, string(domain.StatusRunning), def)
	if err := s.SetOK("x-run"); err == nil {
		t.Fatal("SetOK em RUNNING deveria falhar")
	}
	seedInst(t, s, "x-ok", today, string(domain.StatusOK), def)
	if err := s.SetOK("x-ok"); err == nil {
		t.Fatal("SetOK em OK deveria falhar")
	}
}
