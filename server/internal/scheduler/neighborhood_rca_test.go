package scheduler

import (
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// Grafo comum:  raiz → meio → alvo → filho1, filho2
//                            avô ─┘ (raiz de meio)
func seedChain(t *testing.T, s *Scheduler, date string) {
	t.Helper()
	up := func(from string, c domain.EdgeCondition) []domain.Upstream {
		return []domain.Upstream{{From: from, Condition: c}}
	}
	avo := domain.JobDefinition{ID: "avo", Team: "T0", JobType: "COMMAND"}
	meio := domain.JobDefinition{ID: "meio", Team: "T1", JobType: "COMMAND", Upstream: up("avo", domain.CondOnSuccess)}
	alvo := domain.JobDefinition{ID: "alvo", Team: "T2", JobType: "COMMAND", Upstream: up("meio", domain.CondOnSuccess)}
	filho1 := domain.JobDefinition{ID: "filho1", Team: "T3", JobType: "COMMAND", Upstream: up("alvo", domain.CondOnSuccess), SLA: &domain.SLASpec{DeadlineHM: "08:00"}}
	filho2 := domain.JobDefinition{ID: "filho2", Team: "T3", JobType: "COMMAND", Upstream: up("alvo", domain.CondOnComplete)}
	s.mu.Lock()
	s.defs = []domain.JobDefinition{avo, meio, alvo, filho1, filho2}
	s.mu.Unlock()
}

// Neighborhood radius=1: só os vizinhos DIRETOS (meio upstream; filho1/filho2 downstream).
func TestNeighborhood_Radius1(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-06-24"
	seedChain(t, s, date)
	for _, id := range []string{"avo", "meio", "alvo", "filho1", "filho2"} {
		seedInstStatus(t, s, date, string(domain.StatusWaiting), domain.JobDefinition{ID: id})
	}

	nb, err := s.Neighborhood("alvo-"+date, 1)
	if err != nil {
		t.Fatalf("Neighborhood: %v", err)
	}
	if len(nb.Upstream) != 1 || nb.Upstream[0].DefID != "meio" {
		t.Fatalf("upstream direto esperado [meio], veio %+v", nb.Upstream)
	}
	if nb.Upstream[0].Condition != string(domain.CondOnSuccess) {
		t.Errorf("condição da aresta upstream não rotulada: %q", nb.Upstream[0].Condition)
	}
	down := map[string]bool{}
	for _, n := range nb.Downstream {
		down[n.DefID] = true
	}
	if len(nb.Downstream) != 2 || !down["filho1"] || !down["filho2"] {
		t.Fatalf("downstream direto esperado [filho1,filho2], veio %+v", nb.Downstream)
	}
}

// Neighborhood radius=2: alcança o avô (2 saltos upstream); não passa disso.
func TestNeighborhood_Radius2ReachesGrandparent(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-06-24"
	seedChain(t, s, date)
	for _, id := range []string{"avo", "meio", "alvo", "filho1", "filho2"} {
		seedInstStatus(t, s, date, string(domain.StatusWaiting), domain.JobDefinition{ID: id})
	}

	nb, err := s.Neighborhood("alvo-"+date, 2)
	if err != nil {
		t.Fatalf("Neighborhood: %v", err)
	}
	up := map[string]int{}
	for _, n := range nb.Upstream {
		up[n.DefID] = n.Depth
	}
	if up["meio"] != 1 || up["avo"] != 2 {
		t.Fatalf("esperava meio@1 e avo@2, veio %+v", up)
	}
}

// RCA: alvo bloqueado (WAITING) porque meio NOTOK, que por sua vez falhou porque avô
// NOTOK → causa raiz = avô (falhou por conta própria).
func TestRCA_DeepestRoot(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-06-24"
	seedChain(t, s, date)
	seedInstStatus(t, s, date, string(domain.StatusNotOK), domain.JobDefinition{ID: "avo"})
	seedInstStatus(t, s, date, string(domain.StatusNotOK), domain.JobDefinition{ID: "meio"})
	seedInstStatus(t, s, date, string(domain.StatusWaiting), domain.JobDefinition{ID: "alvo"})

	rca, err := s.RCA("alvo-" + date)
	if err != nil {
		t.Fatalf("RCA: %v", err)
	}
	if len(rca.Roots) != 1 || rca.Roots[0].DefID != "avo" {
		t.Fatalf("causa raiz esperada 'avo', veio %+v (summary=%q)", rca.Roots, rca.Summary)
	}
	if rca.Roots[0].Status != string(domain.StatusNotOK) {
		t.Errorf("raiz deveria estar NOTOK, veio %s", rca.Roots[0].Status)
	}
	// Chain do alvo até a raiz: alvo → meio → avo.
	if len(rca.Chain) < 3 || rca.Chain[0] != "alvo" || rca.Chain[len(rca.Chain)-1] != "avo" {
		t.Errorf("chain esperada alvo..avo, veio %v", rca.Chain)
	}
}

// RCA: alvo é a PRÓPRIA causa raiz (falhou sem upstream falho).
func TestRCA_SelfRoot(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-06-24"
	seedChain(t, s, date)
	seedInstStatus(t, s, date, string(domain.StatusOK), domain.JobDefinition{ID: "avo"})
	seedInstStatus(t, s, date, string(domain.StatusOK), domain.JobDefinition{ID: "meio"})
	seedInstStatus(t, s, date, string(domain.StatusNotOK), domain.JobDefinition{ID: "alvo"})

	rca, err := s.RCA("alvo-" + date)
	if err != nil {
		t.Fatalf("RCA: %v", err)
	}
	if len(rca.Roots) != 1 || rca.Roots[0].DefID != "alvo" || rca.Roots[0].Depth != 0 {
		t.Fatalf("esperava o próprio alvo como raiz (depth 0), veio %+v", rca.Roots)
	}
}

// RCA: job OK → sem causa raiz a rastrear.
func TestRCA_NoFailureNoRoot(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-06-24"
	seedChain(t, s, date)
	seedInstStatus(t, s, date, string(domain.StatusOK), domain.JobDefinition{ID: "alvo"})

	rca, err := s.RCA("alvo-" + date)
	if err != nil {
		t.Fatalf("RCA: %v", err)
	}
	if len(rca.Roots) != 0 {
		t.Fatalf("job OK não deveria ter causa raiz, veio %+v", rca.Roots)
	}
}
