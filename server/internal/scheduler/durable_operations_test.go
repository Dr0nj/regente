package scheduler

import (
	"errors"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"testing"
)

func TestI10OperationsNeverOverwriteUnresolvedAttempt(t *testing.T) {
	f := durableTest(t, domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Resources: map[string]int{"slot": 1}, Params: map[string]interface{}{"command": "effect"}})
	a := f.running(t, "job-2026-09-30")
	for _, action := range []string{"hold", "rerun", "set-ok", "force", "delete"} {
		if _, err := f.s.DurableAction("operator", "job-2026-09-30", action); !errors.Is(err, execution.ErrConflict) {
			t.Fatal(action, err)
		}
	}
	if err := f.s.DurableResourceChange("slot", 0, true); !errors.Is(err, execution.ErrConflict) {
		t.Fatal("deleted held resource", err)
	}
	states, err := f.s.DurableResourceSnapshot()
	if err != nil || len(states) != 1 || states[0].Used != 1 {
		t.Fatal("wrong DB resource snapshot", states, err)
	}
	f.e.AcknowledgeReason(executionIdentity(a), "uncertain", "remote receipt lost")
	f.reopen(t)
	rep, err := f.s.BuildDailyReport("2026-09-30")
	if err != nil || rep.Closed || rep.Counts.Uncertain != 1 {
		t.Fatal("unknown effect closed the day", rep, err)
	}
	for _, action := range []string{"hold", "rerun", "set-ok", "force", "delete"} {
		if _, err := f.s.DurableAction("operator", "job-2026-09-30", action); !errors.Is(err, execution.ErrConflict) {
			t.Fatal(action, err)
		}
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_resource_holds WHERE instance_id=?", "job-2026-09-30") != 1 {
		t.Fatal("unknown effect lost its resource")
	}
	if !carryDecision("UNCERTAIN", false, 100, 100, domain.JobDefinition{}).carry {
		t.Fatal("unknown effect expired")
	}
	if _, err := f.e.Resolve(a.ExecutionID, a.Fence, "resolve", "operator", "failed", "remote operation verified stopped", true); err != nil {
		t.Fatal(err)
	}
	r, err := f.e.Acknowledge(executionIdentity(a), "status")
	if err != nil || !r.Reconciled || r.State != "failed" {
		t.Fatal(r, err)
	}
	if _, err = f.s.DurableAction("operator", "job-2026-09-30", "rerun"); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_resource_holds") != 0 {
		t.Fatal("resolved resource retained")
	}
}
func TestI10EffectResolutionRequiresEvidenceAndAudit(t *testing.T) {
	f := durableTest(t, domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Params: map[string]interface{}{"command": "effect"}})
	a := f.running(t, "job-2026-09-30")
	if _, err := f.e.Complete(execution.Result{Identity: executionIdentity(a), Output: "GLOBAL=1"}); err != nil {
		t.Fatal(err)
	}
	id := a.ExecutionID + "-hooks"
	f.d.Exec("UPDATE execution_effects SET state='uncertain',generation=1 WHERE id=?", id)
	if err := f.s.ResolveEffect(id, "repeat", "operator", "retry", "external state checked", "idempotent", true, false, 1); !errors.Is(err, execution.ErrInvalid) {
		t.Fatal("risk missing", err)
	}
	if err := f.s.ResolveEffect(id, "repeat", "operator", "retry", "external state checked", "idempotent", true, true, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ResolveEffect(id, "repeat", "operator", "retry", "external state checked", "idempotent", true, true, 1); err != nil {
		t.Fatal("same-key replay", err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_effect_decisions WHERE effect_id=?", id) != 1 {
		t.Fatal("duplicate decision")
	}
	if err := f.s.ResolveEffect(id, "stale", "operator", "cancelled", "stale view", "", true, false, 1); !errors.Is(err, execution.ErrConflict) {
		t.Fatal("stale operator overwrote generation", err)
	}
	res, err := f.d.Exec("UPDATE execution_effects SET state='done' WHERE id=? AND generation=1", id)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := res.RowsAffected()
	if n != 0 {
		t.Fatal("old worker overwrote new generation")
	}
}
