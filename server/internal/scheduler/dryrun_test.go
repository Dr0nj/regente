package scheduler

import (
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/storage"
)

func outcomeByDef(dr DryRun) map[string]DryRunOutcome {
	m := map[string]DryRunOutcome{}
	for _, j := range dr.Jobs {
		m[j.DefID] = j.Outcome
	}
	return m
}

// Cobre RUN / WAIT / BLOCKED(direto) / BLOCKED(transitivo) / always / conditions,
// tudo sem calStore (todos os habilitados são "agendados").
func TestDryRun_Classification(t *testing.T) {
	s := newTestScheduler(t)
	s.AttachConditions(NewConditionEngine(s.db))
	en := func(d domain.JobDefinition) domain.JobDefinition { d.Schedule.Enabled = true; return d }
	up := func(from string, c domain.EdgeCondition) []domain.Upstream {
		return []domain.Upstream{{From: from, Condition: c}}
	}

	defs := []domain.JobDefinition{
		en(domain.JobDefinition{ID: "A"}),                                           // RUN (raiz)
		en(domain.JobDefinition{ID: "B", Upstream: up("A", domain.CondOnSuccess)}),  // WAIT (depois de A)
		{ID: "X", Schedule: domain.Schedule{Enabled: false}},                        // DESABILITADO → não agendado
		en(domain.JobDefinition{ID: "C", Upstream: up("X", domain.CondOnSuccess)}),  // BLOCKED (X não roda)
		en(domain.JobDefinition{ID: "D", Upstream: up("C", domain.CondOnComplete)}), // BLOCKED transitivo (C)
		en(domain.JobDefinition{ID: "E", Upstream: up("X", domain.CondAlways)}),     // RUN (always não bloqueia)
		en(domain.JobDefinition{ID: "F", ConditionsIn: []string{"ORPH"}}),           // BLOCKED (condition órfã)
		en(domain.JobDefinition{ID: "H", ConditionsOutAdd: []string{"PROD"}}),       // RUN (produz PROD)
		en(domain.JobDefinition{ID: "G", ConditionsIn: []string{"PROD"}}),           // RUN (PROD é produzido)
	}
	s.mu.Lock()
	s.defs = defs
	s.mu.Unlock()

	dr, err := s.DryRun("2026-12-25")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	m := outcomeByDef(dr)
	want := map[string]DryRunOutcome{
		"A": DryRunRun, "B": DryRunWait, "C": DryRunBlocked, "D": DryRunBlocked,
		"E": DryRunRun, "F": DryRunBlocked, "H": DryRunRun, "G": DryRunRun,
	}
	for id, w := range want {
		if m[id] != w {
			t.Errorf("def %s: outcome=%s, esperava %s", id, m[id], w)
		}
	}
	if _, ok := m["X"]; ok {
		t.Error("X (desabilitado) não deveria aparecer no dry run")
	}
	if dr.Counts.Run != 4 || dr.Counts.Wait != 1 || dr.Counts.Blocked != 3 || dr.Counts.Total != 8 {
		t.Fatalf("counts errados: %+v", dr.Counts)
	}
	if dr.Counts.NotScheduled != 0 {
		t.Fatalf("sem calStore, NotScheduled deveria ser 0, veio %d", dr.Counts.NotScheduled)
	}
}

// Com calStore, a frequência decide quem ENTRA na daily da data (NOT_SCHEDULED).
func TestDryRun_NotScheduledByCalendar(t *testing.T) {
	s := newTestScheduler(t)
	s.AttachCalendars(storage.NewCalendarStore(t.TempDir()))
	s.mu.Lock()
	s.defs = []domain.JobDefinition{
		{ID: "ns", Schedule: domain.Schedule{Enabled: true, Frequency: "monthly", DaysOfMonth: []int{15}}}, // dia 15
		{ID: "sc", Schedule: domain.Schedule{Enabled: true, Frequency: "monthly", DaysOfMonth: []int{25}}}, // dia 25
	}
	s.mu.Unlock()

	dr, err := s.DryRun("2026-12-25") // dia 25
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	m := outcomeByDef(dr)
	if m["ns"] != DryRunNotScheduled {
		t.Fatalf("ns (dia 15) deveria ser NOT_SCHEDULED no dia 25, veio %s", m["ns"])
	}
	if m["sc"] != DryRunRun {
		t.Fatalf("sc (dia 25) deveria RODAR no dia 25, veio %s", m["sc"])
	}
	if dr.Counts.NotScheduled != 1 || dr.Counts.Run != 1 {
		t.Fatalf("counts errados: %+v", dr.Counts)
	}
}

func TestDryRun_BadDate(t *testing.T) {
	s := newTestScheduler(t)
	if _, err := s.DryRun("25/12/2026"); err == nil {
		t.Fatal("data inválida deveria dar erro")
	}
}
