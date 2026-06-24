package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func seedInstStatus(t *testing.T, s *Scheduler, date, status string, def domain.JobDefinition) {
	t.Helper()
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at, definition_snapshot)
		 VALUES(?,?,?,?,?,?,?)`,
		def.ID+"-"+date, def.ID, def.Team, date, status, time.Now(), string(snap),
	); err != nil {
		t.Fatalf("seed %s: %v", def.ID, err)
	}
}

func nodeIDs(ns []BlastNode) map[string]BlastNode {
	m := map[string]BlastNode{}
	for _, n := range ns {
		m[n.DefID] = n
	}
	return m
}

// Grafo:  A → B(on-success) → C(on-complete, SLA)
//
//	A → D(always)            (não propaga)
//	A → E(on-success)=OK     (já rodou → para a cascata)
//	E → F(on-success)        (depende de E, não de A → não impactado)
func TestBlastRadius_CascadeAndStops(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-06-24"
	up := func(from string, c domain.EdgeCondition) []domain.Upstream {
		return []domain.Upstream{{From: from, Condition: c}}
	}
	defA := domain.JobDefinition{ID: "A", Team: "T0", JobType: "COMMAND"}
	defB := domain.JobDefinition{ID: "B", Team: "T1", JobType: "COMMAND", Upstream: up("A", domain.CondOnSuccess)}
	defC := domain.JobDefinition{ID: "C", Team: "T2", JobType: "COMMAND", Upstream: up("B", domain.CondOnComplete), SLA: &domain.SLASpec{DeadlineHM: "08:00"}}
	defD := domain.JobDefinition{ID: "D", Team: "T1", JobType: "COMMAND", Upstream: up("A", domain.CondAlways)}
	defE := domain.JobDefinition{ID: "E", Team: "T3", JobType: "COMMAND", Upstream: up("A", domain.CondOnSuccess)}
	defF := domain.JobDefinition{ID: "F", Team: "T3", JobType: "COMMAND", Upstream: up("E", domain.CondOnSuccess)}

	s.mu.Lock()
	s.defs = []domain.JobDefinition{defA, defB, defC, defD, defE, defF}
	s.mu.Unlock()

	seedInstStatus(t, s, date, string(domain.StatusWaiting), defA)
	seedInstStatus(t, s, date, string(domain.StatusWaiting), defB)
	seedInstStatus(t, s, date, string(domain.StatusWaiting), defC)
	seedInstStatus(t, s, date, string(domain.StatusWaiting), defD)
	seedInstStatus(t, s, date, string(domain.StatusOK), defE) // já rodou
	seedInstStatus(t, s, date, string(domain.StatusWaiting), defF)

	br, err := s.BlastRadius("A-" + date)
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}

	if br.Counts.Downstream != 2 {
		t.Fatalf("downstream esperado=2 (B,C), veio %d (%+v)", br.Counts.Downstream, br.Downstream)
	}
	m := nodeIDs(br.Downstream)
	if _, ok := m["B"]; !ok {
		t.Fatal("B deveria estar no raio (on-success de A, WAITING)")
	}
	if c, ok := m["C"]; !ok || c.Depth != 2 {
		t.Fatalf("C deveria estar no raio em depth 2, veio %+v", c)
	}
	if _, ok := m["D"]; ok {
		t.Fatal("D (aresta always) NÃO deveria ser impactado")
	}
	if _, ok := m["E"]; ok {
		t.Fatal("E (já OK) NÃO deveria ser impactado")
	}
	if _, ok := m["F"]; ok {
		t.Fatal("F (depende de E que já rodou) NÃO deveria ser impactado")
	}
	if br.Counts.SLAAtRisk != 1 || len(br.SLAAtRisk) != 1 || br.SLAAtRisk[0].DefID != "C" {
		t.Fatalf("SLA em risco esperado=[C], veio %+v", br.SLAAtRisk)
	}
	if br.Counts.TeamsAffected != 2 { // T1 (B) + T2 (C)
		t.Fatalf("teams afetados esperado=2, veio %d", br.Counts.TeamsAffected)
	}
	if br.Counts.MaxDepth != 2 {
		t.Fatalf("maxDepth esperado=2, veio %d", br.Counts.MaxDepth)
	}
}

func TestBlastRadius_LeafNoImpact(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-06-24"
	leaf := domain.JobDefinition{ID: "leaf", Team: "T", JobType: "COMMAND"}
	s.mu.Lock()
	s.defs = []domain.JobDefinition{leaf}
	s.mu.Unlock()
	seedInstStatus(t, s, date, string(domain.StatusWaiting), leaf)

	br, err := s.BlastRadius("leaf-" + date)
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if br.Counts.Downstream != 0 || len(br.Downstream) != 0 {
		t.Fatalf("folha não deveria ter downstream, veio %+v", br.Counts)
	}
}

// HELD também é "ainda não rodou" → propaga.
func TestBlastRadius_HeldPropagates(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-06-24"
	a := domain.JobDefinition{ID: "a", Team: "T", JobType: "COMMAND"}
	b := domain.JobDefinition{ID: "b", Team: "T", JobType: "COMMAND", Upstream: []domain.Upstream{{From: "a", Condition: domain.CondOnSuccess}}}
	s.mu.Lock()
	s.defs = []domain.JobDefinition{a, b}
	s.mu.Unlock()
	seedInstStatus(t, s, date, string(domain.StatusWaiting), a)
	seedInstStatus(t, s, date, string(domain.StatusHeld), b)

	br, _ := s.BlastRadius("a-" + date)
	if br.Counts.Downstream != 1 || br.Downstream[0].DefID != "b" {
		t.Fatalf("HELD downstream deveria ser impactado, veio %+v", br.Downstream)
	}
}

func TestBlastRadius_NotFound(t *testing.T) {
	s := newTestScheduler(t)
	if _, err := s.BlastRadius("nope"); err == nil {
		t.Fatal("instance inexistente deveria dar erro")
	}
}
