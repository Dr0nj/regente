package scheduler

import (
	"encoding/json"
	"errors"
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"github.com/Dr0nj/regente-server/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type durableFixture struct {
	dialect db.Dialect
	d       *db.DB
	s       *Scheduler
	e       *execution.Engine
	store   *storage.FileStore
	path    string
	now     time.Time
}

func durableTest(t *testing.T, defs ...domain.JobDefinition) *durableFixture {
	t.Helper()
	f := &durableFixture{path: filepath.Join(t.TempDir(), "runtime.db"), now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), store: storage.NewFileStore(t.TempDir(), false)}
	var err error
	if strings.Contains(t.Name(), "TestI10PostgresRuntimeContracts") || strings.Contains(t.Name(), "TestI11Postgres") {
		f.dialect = db.Postgres
		f.path = durablePGDSN(t)
	} else {
		f.dialect = db.SQLite
	}
	f.d, err = db.Open(f.dialect, f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if f.s != nil {
			f.s.Stop()
		}
		f.d.Close()
	})
	if err = db.Migrate(f.d); err != nil {
		t.Fatal(err)
	}
	for _, def := range defs {
		def.Schedule.Enabled = true
		if err = f.store.Save(def); err != nil {
			t.Fatal(err)
		}
	}
	f.attach()
	f.s.ReloadDefs()
	f.s.RunDaily("2026-09-30")
	f.d.Exec("INSERT INTO machine_principals(agent_id,environment,capabilities) VALUES('worker','','COMMAND,EXECUTION_V2')")
	f.d.Exec("INSERT INTO agent_tokens(token_hash,label,agent_id,expires_at) VALUES('fixture','worker','worker',?)", time.Now().Add(time.Hour).UnixMilli())

	if err = f.s.ReportAgentCapacity("worker", 4, 128, true); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *durableFixture) attach() {
	f.s = New(f.store, f.d, hub.New(), time.Hour)
	f.s.SetClock(businessclock.Func(func() time.Time { return f.now }))
	f.s.RuntimePolicy = runtimeprofile.Config{Profile: "development", ExecutionMode: "durable"}
	f.e = execution.New(f.d, f.s.Now)
	f.s.AttachDurable(f.e)
	rt := NewResourceTracker()
	rt.LoadFromDB(f.d)
	f.s.AttachResources(rt)
}
func (f *durableFixture) reopen(t *testing.T) {
	t.Helper()
	f.s.Stop()
	f.d.Close()
	var err error
	f.d, err = db.Open(f.dialect, f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.attach()
	f.s.ReloadDefs()
}
func (f *durableFixture) running(t *testing.T, id string) execution.Attempt {
	t.Helper()
	var raw string
	if err := f.d.QueryRow("SELECT definition_snapshot FROM instances WHERE id=?", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var def domain.JobDefinition
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		t.Fatal(err)
	}
	f.s.startInstance(id, def)
	order, err := f.e.RuntimeOrder(id)
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.e.Attempt(order.CurrentExecution)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := f.e.Claim(a.AgentID)
	if err != nil || msg == nil {
		t.Fatal(msg, err)
	}
	for _, kind := range []string{"accepted", "started"} {
		if _, err = f.e.Acknowledge(executionIdentity(a), kind); err != nil {
			t.Fatal(kind, err)
		}
	}
	return a
}
func executionIdentity(a execution.Attempt) execution.Identity {
	return execution.Identity{Protocol: 2, ExecutionID: a.ExecutionID, Fence: a.Fence, AgentID: a.AgentID}
}
func scalar(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func instanceState(t *testing.T, f *durableFixture, id string) string {
	t.Helper()
	var state string
	if err := f.d.QueryRow("SELECT status FROM instances WHERE id=?", id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestI10RealOrderResultConditionsAndActionsAtomicRecovery(t *testing.T) {
	var deliveries atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing effect identity")
		}
		deliveries.Add(1)
		w.WriteHeader(200)
	}))
	defer sink.Close()
	f := durableTest(t, domain.JobDefinition{ID: "parent", Team: "test", JobType: "COMMAND", Params: map[string]interface{}{"command": "effect"}, ConditionsIn: []string{"input"}, ConditionsOutAdd: []string{"output"}, ConditionsOutRemove: []string{"input"}, Actions: []domain.ActionRule{{On: "result", Status: "OK", Do: "run-job", TargetJob: "child"}, {On: "result", Status: "OK", Do: "notify", Message: "done", Channels: []string{"webhook"}}}}, domain.JobDefinition{ID: "child", Team: "test", JobType: "COMMAND", Confirm: true, Params: map[string]interface{}{"command": "child"}})
	f.d.Exec("INSERT INTO settings(key,value) VALUES('alert_webhook_url',?)", sink.URL)
	f.s.conditions.Set("input", "2026-09-30", "fixture")
	a := f.running(t, "parent-2026-09-30")
	r := execution.Result{Identity: executionIdentity(a), Output: "done"}
	dropCondition := durableFault(t, f.d, "conditions", "condition_fault")
	if _, err := f.e.Complete(r); err == nil {
		t.Fatal("condition write failure accepted result")
	}
	state, _ := f.e.Attempt(a.ExecutionID)
	if state.State != "running" || instanceState(t, f, "parent-2026-09-30") != "RUNNING" || !f.s.conditions.Has("input", "2026-09-30") || scalar(t, f.d, "SELECT COUNT(*) FROM execution_effects") != 0 {
		t.Fatal("partial terminal transaction")
	}
	dropCondition()
	dropAction := durableFault(t, f.d, "execution_effects", "action_fault")
	if _, err := f.e.Complete(r); err == nil {
		t.Fatal("action intent failure accepted result")
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM alert_events") != 0 || f.s.conditions.Has("output", "2026-09-30") {
		t.Fatal("partial notification or condition")
	}
	dropAction()
	if _, err := f.e.Complete(r); err != nil {
		t.Fatal(err)
	}
	if instanceState(t, f, "parent-2026-09-30") != "OK" || !f.s.conditions.Has("output", "2026-09-30") || f.s.conditions.Has("input", "2026-09-30") || deliveries.Load() != 0 {
		t.Fatal("terminal semantics")
	}
	f.reopen(t)
	f.s.drainDurableEffects()
	if deliveries.Load() != 1 || scalar(t, f.d, "SELECT COUNT(*) FROM instances WHERE id LIKE 'child-ACTION-%'") != 1 || scalar(t, f.d, "SELECT COUNT(*) FROM execution_effects WHERE state!='done'") != 0 {
		t.Fatal("post-restart effects missing or duplicated", deliveries.Load())
	}
	receipt, err := f.e.Complete(r)
	if err != nil || !receipt.Duplicate {
		t.Fatal(receipt, err)
	}
	f.s.drainDurableEffects()
	if deliveries.Load() != 1 {
		t.Fatal("duplicate completion repeated notification")
	}
}
func TestI10UnknownEffectRequiresAuditedResolutionAndNeverAutoRetries(t *testing.T) {
	f := durableTest(t, domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Retries: 3, Params: map[string]interface{}{"command": "effect"}})
	a := f.running(t, "job-2026-09-30")
	f.now = f.now.Add(6 * time.Minute)
	if n, err := f.e.Reconcile(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if instanceState(t, f, "job-2026-09-30") != "UNCERTAIN" {
		t.Fatal("unknown classified as failure")
	}
	f.s.tickOnce()
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_attempts") != 1 {
		t.Fatal("unknown effect auto-retried")
	}
	if _, err := f.e.Resolve(a.ExecutionID, a.Fence, "denied", "operator", "failed", "verified effect", false); !errors.Is(err, execution.ErrInvalid) {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	out := make(chan error, 2)
	var wg sync.WaitGroup
	for _, decision := range []string{"succeeded", "failed"} {
		wg.Add(1)
		go func(decision string) {
			defer wg.Done()
			<-gate
			_, err := f.e.Resolve(a.ExecutionID, a.Fence, "resolve-"+decision, "operator", decision, "effect stopped; evidence verified", true)
			out <- err
		}(decision)
	}
	close(gate)
	wg.Wait()
	close(out)
	success, conflict := 0, 0
	for err := range out {
		if err == nil {
			success++
		} else if errors.Is(err, execution.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 || scalar(t, f.d, "SELECT COUNT(*) FROM execution_decisions") != 1 {
		t.Fatal(success, conflict)
	}
	f.s.tickOnce()
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_attempts") != 1 {
		t.Fatal("operator resolution auto-retried")
	}
	f.s.FinishInstance("job-2026-09-30", domain.StatusNotOK, 5, "old v1")
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_attempts") != 1 {
		t.Fatal("v1 bypass")
	}
}
func TestI10ExternalActionUnknownIsNotRepeatedAfterRestart(t *testing.T) {
	f := durableTest(t, domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Params: map[string]interface{}{"command": "effect"}, Actions: []domain.ActionRule{{On: "result", Status: "OK", Do: "notify", Message: "done"}}})
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer sink.Close()
	f.d.Exec("INSERT INTO settings(key,value) VALUES('alert_webhook_url',?)", sink.URL)
	a := f.running(t, "job-2026-09-30")
	if _, err := f.e.Complete(execution.Result{Identity: executionIdentity(a)}); err != nil {
		t.Fatal(err)
	}
	f.d.Exec("UPDATE execution_effects SET state='claimed',lease_until=? WHERE kind='notify'", f.now.Add(-time.Second).UnixMilli())
	f.reopen(t)
	f.s.drainDurableEffects()
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_effects WHERE kind='notify' AND state='uncertain'") != 1 {
		t.Fatal("claimed effect blindly replayed")
	}
}
func TestI10KnownFailureRetryIsDurableAndOldAttemptCannotFinishNext(t *testing.T) {
	f := durableTest(t, domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Retries: 1, Params: map[string]interface{}{"command": "effect"}})
	first := f.running(t, "job-2026-09-30")
	r := execution.Result{Identity: executionIdentity(first), ExitCode: 1, Output: "failed"}
	if _, err := f.e.Complete(r); err != nil {
		t.Fatal(err)
	}
	if instanceState(t, f, "job-2026-09-30") != "WAITING" {
		t.Fatal("known failure not scheduled")
	}
	f.reopen(t)
	f.now = f.now.Add(6 * time.Second)
	second := f.running(t, "job-2026-09-30")
	if second.Attempt != 2 || second.Fence <= first.Fence {
		t.Fatal(second)
	}
	if _, err := f.e.Complete(r); err != nil {
		t.Fatal(err)
	}
	if instanceState(t, f, "job-2026-09-30") != "RUNNING" {
		t.Fatal("old result overwrote current")
	}
	if _, err := f.e.Complete(execution.Result{Identity: executionIdentity(second), Output: "success"}); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM instance_runs WHERE finished_at IS NOT NULL") != 2 {
		t.Fatal("attempt history lost")
	}
}
