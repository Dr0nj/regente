package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/domain"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestI10RuntimeActionsAndSLARecoverAtomically(t *testing.T) {
	var effects atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing effect identity")
		}
		effects.Add(1)
		w.WriteHeader(200)
	}))
	defer sink.Close()
	f := durableTest(t, domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Params: map[string]interface{}{"command": "work"}, SLA: &domain.SLASpec{ExpectedDurationMin: 1, WebhookURL: sink.URL}, Actions: []domain.ActionRule{{On: "runtime", AfterMin: 1, Do: "set-condition", Condition: "runtime-proof"}, {On: "runtime", AfterMin: 1, Do: "notify", Channels: []string{"webhook"}, Message: "running"}}})
	f.d.Exec("INSERT INTO settings(key,value) VALUES('alert_webhook_url',?)", sink.URL)
	f.s.sla = NewSLAEngine(f.d, nil)
	f.running(t, "job-2026-09-30")
	f.now = f.now.Add(2 * time.Minute)
	remove := durableFault(t, f.d, "execution_effects", "runtime_fault")
	f.s.evaluateDurableRunning(f.now)
	if scalar(t, f.d, "SELECT COUNT(*) FROM conditions WHERE name='runtime-proof'") != 0 || scalar(t, f.d, "SELECT COUNT(*) FROM alert_events") != 0 || effects.Load() != 0 {
		t.Fatal("partial runtime commit")
	}
	remove()
	f.s.evaluateDurableRunning(f.now)
	f.s.evaluateDurableRunning(f.now)
	if scalar(t, f.d, "SELECT COUNT(*) FROM conditions WHERE name='runtime-proof'") != 1 || scalar(t, f.d, "SELECT COUNT(*) FROM sla_breaches") != 1 || scalar(t, f.d, "SELECT COUNT(*) FROM alert_events") != 1 || effects.Load() != 0 {
		t.Fatal("runtime intent missing or duplicated")
	}
	f.reopen(t)
	f.s.drainDurableEffects()
	f.s.evaluateDurableRunning(f.now)
	f.s.drainDurableEffects()
	if effects.Load() != 2 || scalar(t, f.d, "SELECT COUNT(*) FROM sla_breaches WHERE notified=1") != 1 {
		t.Fatal("runtime delivery lost or duplicated", effects.Load())
	}
}
