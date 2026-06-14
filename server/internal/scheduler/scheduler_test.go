package scheduler

import (
	"encoding/json"
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// Fase A — imutabilidade: defForInstance deve preferir o snapshot congelado e
// só cair na def viva quando não há snapshot (instances legadas).
func TestDefForInstance_PrefersSnapshot(t *testing.T) {
	live := map[string]domain.JobDefinition{"x": {ID: "x", Label: "LIVE"}}

	snap, _ := json.Marshal(domain.JobDefinition{ID: "x", Label: "FROZEN"})
	d, ok := defForInstance(instRow{DefID: "x", Snapshot: string(snap)}, live)
	if !ok || d.Label != "FROZEN" {
		t.Fatalf("esperava snapshot FROZEN, veio ok=%v label=%q", ok, d.Label)
	}

	d2, ok2 := defForInstance(instRow{DefID: "x"}, live)
	if !ok2 || d2.Label != "LIVE" {
		t.Fatalf("sem snapshot deveria cair na def viva LIVE, veio ok=%v label=%q", ok2, d2.Label)
	}

	if _, ok3 := defForInstance(instRow{DefID: "inexistente"}, live); ok3 {
		t.Fatal("def inexistente sem snapshot deveria retornar ok=false")
	}
}
