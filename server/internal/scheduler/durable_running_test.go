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

func TestRuntimePrefilterKeepsThresholdsAndDynamicSlowRules(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	def := domain.JobDefinition{ID: "job", Actions: []domain.ActionRule{{On: "runtime", AfterMin: 1}}}
	if runtimeEvaluationDue(def, "2026-09-30", now.Add(-59*time.Second).UnixMilli(), now, true, false, nil) {
		t.Fatal("ação antes do limiar")
	}
	if !runtimeEvaluationDue(def, "2026-09-30", now.Add(-time.Minute).UnixMilli(), now, true, false, nil) {
		t.Fatal("ação perdida no limiar inclusivo")
	}
	def.Actions = nil
	def.SLA = &domain.SLASpec{ExpectedDurationMin: 1}
	if runtimeEvaluationDue(def, "2026-09-30", now.Add(-time.Minute).UnixMilli(), now, true, false, nil) {
		t.Fatal("SLA adiantado no limiar estrito")
	}
	if !runtimeEvaluationDue(def, "2026-09-30", now.Add(-time.Minute-time.Millisecond).UnixMilli(), now, true, false, nil) {
		t.Fatal("SLA vencido perdido")
	}
	def.SLA = nil
	rules := []AlertRule{{WorkflowPattern: "job"}}
	if runtimeEvaluationDue(def, "2026-09-30", now.UnixMilli(), now, true, false, rules) {
		t.Fatal("alerta sem histórico")
	}
	if !runtimeEvaluationDue(def, "2026-09-30", now.UnixMilli(), now, true, true, rules) {
		t.Fatal("histórico/regra novos não despertam avaliação")
	}
	rules[0].WorkflowPattern = "other"
	if runtimeEvaluationDue(def, "2026-09-30", now.UnixMilli(), now, true, true, rules) {
		t.Fatal("regra de outro job")
	}
}

func TestRuntimeNoopDoesNotAcquireExecutionWriteLock(t *testing.T) {
	f := durableTest(t, domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Params: map[string]interface{}{"command": "work"}})
	f.running(t, "job-2026-09-30")
	for _, q := range []string{
		"CREATE TABLE runtime_lock_probe(n INTEGER)",
		"INSERT INTO runtime_lock_probe VALUES(0)",
		"CREATE TRIGGER runtime_lock_probe_trigger AFTER UPDATE ON execution_queue_lock BEGIN UPDATE runtime_lock_probe SET n=n+1; END",
	} {
		if _, err := f.d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	f.s.evaluateDurableRunning(f.now)
	if n := scalar(t, f.d, "SELECT n FROM runtime_lock_probe"); n != 0 {
		t.Fatalf("no-op disputou lock de escrita: %d", n)
	}
}

func TestRuntimePrefilterObservesNewHistoryAndRules(t *testing.T) {
	f := durableTest(t, domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Params: map[string]interface{}{"command": "work"}})
	f.running(t, "job-2026-09-30")
	f.s.alerts = NewAlertEngine(f.d, nil)
	f.s.alerts.SeedDefaults()
	f.now = f.now.Add(time.Minute)
	f.s.evaluateDurableRunning(f.now)
	if n := scalar(t, f.d, "SELECT COUNT(*) FROM alert_events"); n != 0 {
		t.Fatalf("alerta sem histórico: %d", n)
	}
	if _, err := f.d.Exec("UPDATE alert_rules SET enabled=0 WHERE id='rule-slow'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Exec("INSERT INTO instance_runs(instance_id,definition_id,order_date,started_at,finished_at,status) VALUES('prior','job','2026-09-29',?,?,'OK')", f.now.Add(-time.Hour).Format("2006-01-02 15:04:05"), f.now.Add(-time.Hour+time.Second).Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	f.s.evaluateDurableRunning(f.now)
	if n := scalar(t, f.d, "SELECT COUNT(*) FROM alert_events"); n != 0 {
		t.Fatalf("regra desabilitada disparou: %d", n)
	}
	if _, err := f.d.Exec("UPDATE alert_rules SET enabled=1 WHERE id='rule-slow'"); err != nil {
		t.Fatal(err)
	}
	f.s.evaluateDurableRunning(f.now)
	f.s.evaluateDurableRunning(f.now)
	if n := scalar(t, f.d, "SELECT COUNT(*) FROM alert_events WHERE rule_id='rule-slow'"); n != 1 {
		t.Fatalf("novo histórico/regra não observado ou duplicado: %d", n)
	}
}
