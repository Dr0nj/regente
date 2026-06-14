package leader

import "testing"

func TestSingleNodeAlwaysLeader(t *testing.T) {
	var e Elector = SingleNode{}
	if !e.IsLeader() {
		t.Fatal("SingleNode deve ser sempre líder")
	}
}

func TestPgAdvisoryNotLeaderBeforeStart(t *testing.T) {
	// Sem Start (sem conexão adquirida), não é líder — fail-safe.
	p := NewPgAdvisory(nil, DefaultAdvisoryKey, 0)
	if p.IsLeader() {
		t.Fatal("PgAdvisory não pode ser líder antes de adquirir o lock")
	}
	var _ Elector = p // satisfaz a interface
}
