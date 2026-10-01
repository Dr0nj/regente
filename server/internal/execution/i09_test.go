package execution

import "testing"

func TestI09StartReceiptNeverAuthorizesOldOrUncertainAttempt(t *testing.T) {
	f := newFixture(t)
	o := f.order(t, "copy")
	a := f.start(t, o, "first")
	f.running(t, a)
	r, err := f.e.Acknowledge(identity(a), "started")
	if err != nil || !r.StartAuthorized || !r.Current {
		t.Fatal(r, err)
	}
	r, err = f.e.Acknowledge(identity(a), "uncertain")
	if err != nil || r.State != "uncertain" || r.StartAuthorized {
		t.Fatal(r, err)
	}
	r, err = f.e.Acknowledge(identity(a), "started")
	if err != nil || r.StartAuthorized {
		t.Fatal("unknown effect authorized", r, err)
	}
	if _, err = f.e.Complete(Result{Identity: identity(a), ExitCode: 1, Output: "known failure"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.Start(o.ID, a.AgentID, "retry", "retry"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"accepted", "started", "uncertain"} {
		r, err = f.e.Acknowledge(identity(a), kind)
		if err != nil || r.StartAuthorized || r.Current {
			t.Fatal("old execution authorized", kind, r, err)
		}
	}
}
func TestI09CapacityStillDeliversControlIntent(t *testing.T) {
	f := newFixture(t)
	o := f.order(t, "copy")
	a := f.start(t, o, "first")
	if msg, err := f.e.ClaimCapacity(a.AgentID, false); err != nil || msg != nil {
		t.Fatal(msg, err)
	}
	if msg, err := f.e.Claim(a.AgentID); err != nil || msg == nil {
		t.Fatal(msg, err)
	}
	if _, err := f.e.Cancel(o.ID); err != nil {
		t.Fatal(err)
	}
	if msg, err := f.e.ClaimCapacity(a.AgentID, false); err != nil || msg == nil || msg.Kind != "cancel" {
		t.Fatal(msg, err)
	}
}
