package storage

import (
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// TestFileStore_ActionsRoundTrip — uma definition com regras On/Do (Actions)
// sobrevive ao Save→List (YAML marshal/unmarshal com as tags de domain.ActionRule).
// Prova que a UI das actions persiste de verdade no GitOps.
func TestFileStore_ActionsRoundTrip(t *testing.T) {
	store := NewFileStore(t.TempDir(), false)
	def := domain.JobDefinition{
		ID:      "billing",
		Team:    "fin",
		Label:   "Billing",
		JobType: "COMMAND",
		Actions: []domain.ActionRule{
			{On: "result", Status: "NOTOK", Do: "notify", Severity: "critical", Channels: []string{"slack", "pagerduty"}, Message: "billing falhou"},
			{On: "result", Status: "OK", Do: "set-condition", Condition: "BILLING_DONE"},
			{On: "attempt", Attempt: 2, Do: "run-job", TargetJob: "cleanup"},
			{On: "runtime", AfterMin: 30, Do: "notify", Severity: "warning"},
			{On: "result", Status: "NOTOK", Do: "set-ok"},
		},
	}
	if err := store.Save(def); err != nil {
		t.Fatalf("Save: %v", err)
	}

	defs, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var got *domain.JobDefinition
	for i := range defs {
		if defs[i].ID == "billing" {
			got = &defs[i]
		}
	}
	if got == nil {
		t.Fatal("definition não voltou do List")
	}
	if len(got.Actions) != 5 {
		t.Fatalf("esperava 5 regras, veio %d", len(got.Actions))
	}
	r0 := got.Actions[0]
	if r0.On != "result" || r0.Status != "NOTOK" || r0.Do != "notify" || r0.Severity != "critical" {
		t.Fatalf("regra 0 não preservada: %+v", r0)
	}
	if len(r0.Channels) != 2 || r0.Channels[0] != "slack" || r0.Channels[1] != "pagerduty" {
		t.Fatalf("channels não preservados: %v", r0.Channels)
	}
	if got.Actions[2].TargetJob != "cleanup" || got.Actions[2].Attempt != 2 {
		t.Fatalf("run-job não preservado: %+v", got.Actions[2])
	}
	if got.Actions[3].AfterMin != 30 {
		t.Fatalf("runtime afterMin não preservado: %+v", got.Actions[3])
	}
}
