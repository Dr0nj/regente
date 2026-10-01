package execution

import (
	"encoding/json"
	"errors"
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fixture struct {
	e    *Engine
	d    *db.DB
	path string
	now  time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{path: filepath.Join(t.TempDir(), "attempts.db"), now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	var err error
	f.d, err = db.Open(db.SQLite, f.path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(f.d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.d.Close() })
	f.e = New(f.d, func() time.Time { return f.now })
	raw, err := json.Marshal(domain.JobDefinition{ID: "job", JobType: "COMMAND", Params: map[string]interface{}{"command": "original"}, BusinessTime: &businessclock.Calendar{Timezone: "UTC", DailyAt: "00:00"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.d.Exec(`INSERT INTO instances(id,definition_id,team,order_date,status,scheduled_at,definition_snapshot) VALUES('source','job','test','2026-09-30','WAITING',?,?)`, f.now, string(raw)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"agent-a", "agent-b"} {
		if _, err = f.d.Exec(`INSERT INTO machine_principals(agent_id,environment,capabilities) VALUES(?,'','COMMAND,EXECUTION_V2')`, id); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func (f *fixture) order(t *testing.T, key string) Order {
	t.Helper()
	o, err := f.e.CreateOrder("source", key)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func (f *fixture) start(t *testing.T, o Order, key string) Attempt {
	t.Helper()
	a, err := f.e.Start(o.ID, "agent-a", key, "start")
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func identity(a Attempt) Identity {
	return Identity{ProtocolVersion, a.ExecutionID, a.Fence, a.AgentID}
}
func (f *fixture) running(t *testing.T, a Attempt) {
	t.Helper()
	msg, err := f.e.Claim(a.AgentID)
	if err != nil || msg == nil || msg.ExecutionID != a.ExecutionID {
		t.Fatal(msg, err)
	}
	for _, kind := range []string{"accepted", "started"} {
		if _, err = f.e.Acknowledge(identity(a), kind); err != nil {
			t.Fatal(kind, err)
		}
	}
}
func count(t *testing.T, d *db.DB, q string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.d, err = db.Open(db.SQLite, f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.e = New(f.d, func() time.Time { return f.now })
}

func TestI08AtomicIntentAndRestart(t *testing.T) {
	f := newFixture(t)
	o := f.order(t, "copy")
	if _, err := f.d.Exec(`CREATE TRIGGER intent_fault BEFORE INSERT ON execution_outbox BEGIN SELECT RAISE(ABORT,'synthetic intent failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Start(o.ID, "agent-a", "start", "start"); err == nil {
		t.Fatal("não propagou falha da outbox")
	}
	current, _ := f.e.Order(o.ID)
	if current.Fence != 0 || current.Attempt != 0 || count(t, f.d, "SELECT COUNT(*) FROM execution_attempts") != 0 || count(t, f.d, "SELECT COUNT(*) FROM execution_events") != 0 {
		t.Fatal("tentativa parcial", current)
	}
	f.d.Exec("DROP TRIGGER intent_fault")
	f.e.Notify = func(string) { panic("notification lost") }
	a := f.start(t, o, "start")
	f.reopen(t)
	same, err := f.e.Start(o.ID, "agent-a", "start", "start")
	if err != nil || same.ExecutionID != a.ExecutionID {
		t.Fatal(same, err)
	}
	first, err := f.e.Claim("agent-a")
	if err != nil || first == nil {
		t.Fatal(first, err)
	}
	f.reopen(t)
	if msg, err := f.e.Claim("agent-a"); err != nil || msg != nil {
		t.Fatal("claim ainda válido", msg, err)
	}
	f.now = f.now.Add(time.Minute)
	second, err := f.e.Claim("agent-a")
	if err != nil || second == nil || first.MessageID != second.MessageID || first.ExecutionID != second.ExecutionID || first.Fence != second.Fence || second.Definition.Params["command"] != "original" {
		t.Fatal("redelivery mudou identidade/fonte", second, err)
	}
	for _, kind := range []string{"accepted", "started"} {
		if _, err = f.e.Acknowledge(identity(a), kind); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.d.Exec(`CREATE TRIGGER result_fault BEFORE INSERT ON execution_events WHEN NEW.kind='result_recorded' BEGIN SELECT RAISE(ABORT,'synthetic terminal event failure'); END`); err != nil {
		t.Fatal(err)
	}
	result := Result{Identity: identity(a), Output: "done"}
	if _, err = f.e.Complete(result); err == nil {
		t.Fatal("resultado parcial não falhou")
	}
	state, _ := f.e.Attempt(a.ExecutionID)
	if state.State != "running" {
		t.Fatal("resultado perdeu atomicidade", state)
	}
	f.d.Exec("DROP TRIGGER result_fault")
	receipt, err := f.e.Complete(result)
	if err != nil || receipt.Duplicate {
		t.Fatal(receipt, err)
	}
	f.reopen(t)
	receipt, err = f.e.Complete(result)
	if err != nil || !receipt.Duplicate {
		t.Fatal("ACK perdido não idempotente", receipt, err)
	}
	if count(t, f.d, "SELECT COUNT(*) FROM execution_events WHERE kind='result_recorded'") != 1 {
		t.Fatal("resultado reaplicado")
	}
	var sourceStatus string
	f.d.QueryRow("SELECT status FROM instances WHERE id='source'").Scan(&sourceStatus)
	if sourceStatus != "WAITING" || count(t, f.d, "SELECT COUNT(*) FROM conditions") != 0 || count(t, f.d, "SELECT COUNT(*) FROM instance_runs") != 0 {
		t.Fatal("laboratório alterou runtime legado")
	}
}
func TestI08ConcurrentResultAndStart(t *testing.T) {
	f := newFixture(t)
	o := f.order(t, "copy")
	other := New(f.d, func() time.Time { return f.now })
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, e := range []*Engine{f.e, other} {
		wg.Add(1)
		go func(i int, e *Engine) {
			defer wg.Done()
			<-start
			_, err := e.Start(o.ID, "agent-a", string(rune('a'+i)), "start")
			results <- err
		}(i, e)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
	current, _ := f.e.Order(o.ID)
	a, _ := f.e.Attempt(current.CurrentExecution)
	f.running(t, a)
	start = make(chan struct{})
	results = make(chan error, 2)
	for i, e := range []*Engine{f.e, other} {
		wg.Add(1)
		go func(i int, e *Engine) {
			defer wg.Done()
			<-start
			_, err := e.Complete(Result{Identity: identity(a), ExitCode: i, Output: "one result"})
			results <- err
		}(i, e)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts = 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 || count(t, f.d, "SELECT COUNT(*) FROM execution_events WHERE kind='result_recorded'") != 1 {
		t.Fatal("CAS não serializou", success, conflicts)
	}
}
func TestI08OutputAndOldAttempt(t *testing.T) {
	f := newFixture(t)
	o := f.order(t, "copy")
	a := f.start(t, o, "start")
	f.running(t, a)
	chunk := Output{Identity: identity(a), Seq: 1, Chunk: "first"}
	if _, err := f.e.Append(chunk); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	if r, err := f.e.Append(chunk); err != nil || !r.Duplicate {
		t.Fatal(r, err)
	}
	changed := chunk
	changed.Chunk = "altered"
	if _, err := f.e.Append(changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	gap := chunk
	gap.Seq = 3
	if _, err := f.e.Append(gap); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	wrong := chunk
	wrong.AgentID = "agent-b"
	if _, err := f.e.Append(wrong); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	wrong = chunk
	wrong.Fence++
	if _, err := f.e.Append(wrong); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	result := Result{Identity: identity(a), ExitCode: 1, Output: "failed"}
	if _, err := f.e.Complete(result); err != nil {
		t.Fatal(err)
	}
	b, err := f.e.Start(o.ID, "agent-a", "retry", "retry")
	if err != nil || b.ExecutionID == a.ExecutionID || b.Fence <= a.Fence || b.Attempt != 2 {
		t.Fatal(b, err)
	}
	f.running(t, b)
	if _, err = f.e.Append(Output{Identity: identity(a), Seq: 2, Chunk: "late"}); !errors.Is(err, ErrConflict) {
		t.Fatal("output antigo entrou", err)
	}
	if r, err := f.e.Append(chunk); err != nil || !r.Duplicate {
		t.Fatal("ACK de chunk antigo perdido", r, err)
	}
	if r, err := f.e.Complete(result); err != nil || !r.Duplicate {
		t.Fatal(r, err)
	}
	oldResult := result
	oldResult.Output = "different"
	if _, err = f.e.Complete(oldResult); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	state, _ := f.e.Attempt(b.ExecutionID)
	if state.State != "running" {
		t.Fatal("resultado antigo fechou tentativa atual", state)
	}
	out, err := f.e.Output(b.ExecutionID, 0)
	if err != nil || out == nil || len(out) != 0 {
		t.Fatal(out, err)
	}
	f.d.Exec("UPDATE execution_attempts SET output_bytes=? WHERE execution_id=?", MaxOutputBytes, b.ExecutionID)
	f.reopen(t)
	if _, err = f.e.Append(Output{Identity: identity(b), Seq: 1, Chunk: "over cap"}); !errors.Is(err, ErrOutputLimit) {
		t.Fatal("cap perdeu estado no restart", err)
	}
}
func TestI08CancellationAndUncertainty(t *testing.T) {
	f := newFixture(t)
	o := f.order(t, "cancel-before")
	a := f.start(t, o, "start")
	cancelled, err := f.e.Cancel(o.ID)
	if err != nil || cancelled.State != "cancelled" {
		t.Fatal(cancelled, err)
	}
	if msg, err := f.e.Claim("agent-a"); err != nil || msg != nil {
		t.Fatal("cancelado despachou", msg, err)
	}
	if _, err = f.e.Complete(Result{Identity: identity(a)}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	o = f.order(t, "cancel-running")
	a = f.start(t, o, "start")
	f.running(t, a)
	if _, err = f.e.Cancel(o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.Start(o.ID, "agent-a", "rerun", "rerun"); !errors.Is(err, ErrConflict) {
		t.Fatal("cancel pedido autorizou novo efeito", err)
	}
	f.now = f.now.Add(6 * time.Minute)
	if n, err := f.e.Reconcile(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	msg, err := f.e.Claim("agent-a")
	if err != nil || msg == nil || msg.Kind != "cancel" {
		t.Fatal("cancel não recuperável após lease", msg, err)
	}
	if _, err = f.e.Acknowledge(identity(a), "cancelled"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.Complete(Result{Identity: identity(a), Output: "late"}); !errors.Is(err, ErrConflict) {
		t.Fatal("resultado sobrescreveu cancel confirmado", err)
	}
	o = f.order(t, "lease")
	a = f.start(t, o, "start")
	f.running(t, a)
	f.now = f.now.Add(6 * time.Minute)
	if n, err := f.e.Reconcile(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	state, _ := f.e.Attempt(a.ExecutionID)
	if state.State != "uncertain" {
		t.Fatal(state)
	}
	if _, err = f.e.Start(o.ID, "agent-a", "retry", "rerun"); !errors.Is(err, ErrConflict) {
		t.Fatal("lease tratada como processo parado", err)
	}
	if _, err = f.e.Complete(Result{Identity: identity(a), Output: "verified late result"}); err != nil {
		t.Fatal(err)
	}
}
func TestI08CancelVersusResult(t *testing.T) {
	f := newFixture(t)
	o := f.order(t, "race")
	a := f.start(t, o, "start")
	f.running(t, a)
	if _, err := f.e.Cancel(o.ID); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	other := New(f.d, func() time.Time { return f.now })
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := f.e.Complete(Result{Identity: identity(a), Output: "result"})
		results <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := other.Acknowledge(identity(a), "cancelled")
		results <- err
	}()
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	state, _ := f.e.Attempt(a.ExecutionID)
	order, _ := f.e.Order(o.ID)
	if success != 1 || conflicts != 1 || !terminal(state.State) || order.State != state.State {
		t.Fatal(success, conflicts, state, order)
	}
}
func TestI08BackpressureAndDeliveryExhaustion(t *testing.T) {
	f := newFixture(t)
	f.e.MaxPending = 1
	one := f.order(t, "one")
	two := f.order(t, "two")
	a := f.start(t, one, "start")
	if _, err := f.e.Start(two.ID, "agent-a", "start", "start"); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	order, _ := f.e.Order(two.ID)
	if order.Fence != 0 || order.Attempt != 0 {
		t.Fatal("backpressure gravou metade", order)
	}
	f.e.MaxDeliveries = 1
	if msg, err := f.e.Claim("agent-a"); err != nil || msg == nil {
		t.Fatal(msg, err)
	}
	f.now = f.now.Add(time.Minute)
	if msg, err := f.e.Claim("agent-a"); err != nil || msg != nil {
		t.Fatal(msg, err)
	}
	state, _ := f.e.Attempt(a.ExecutionID)
	m, err := f.e.Metrics()
	if err != nil || state.State != "uncertain" || m.Paused != 1 || m.Uncertain != 1 || m.Deliveries != 1 {
		t.Fatal(state, m, err)
	}
	if _, err = f.e.Start(one.ID, "agent-a", "retry", "rerun"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

// Processo real termina sem Close/defer após commit e depois após claim.
func TestI08ProcessExitAfterIntent(t *testing.T) {
	if target := os.Getenv("REGENTE_I08_CRASH_DB"); target != "" {
		d, err := db.Open(db.SQLite, target)
		if err != nil {
			t.Fatal(err)
		}
		e := New(d, func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) })
		if os.Getenv("REGENTE_I08_CRASH_PHASE") == "intent" {
			if _, err = e.Start(os.Getenv("REGENTE_I08_CRASH_ORDER"), "agent-a", "crash", "start"); err != nil {
				t.Fatal(err)
			}
		} else {
			if msg, err := e.Claim("agent-a"); err != nil || msg == nil {
				t.Fatal(msg, err)
			}
		}
		os.Exit(0)
	}
	f := newFixture(t)
	o := f.order(t, "process-exit")
	run := func(phase string) {
		t.Helper()
		if err := f.d.Close(); err != nil {
			t.Fatal(err)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestI08ProcessExitAfterIntent$")
		child.Env = append(os.Environ(), "REGENTE_I08_CRASH_DB="+f.path, "REGENTE_I08_CRASH_ORDER="+o.ID, "REGENTE_I08_CRASH_PHASE="+phase)
		if out, err := child.CombinedOutput(); err != nil {
			t.Fatalf("filho: %v: %s", err, out)
		}
		var err error
		f.d, err = db.Open(db.SQLite, f.path)
		if err != nil {
			t.Fatal(err)
		}
		f.e = New(f.d, func() time.Time { return f.now })
	}
	run("intent")
	order, err := f.e.Order(o.ID)
	if err != nil || order.CurrentExecution == "" || count(t, f.d, "SELECT COUNT(*) FROM execution_outbox WHERE state='pending'") != 1 {
		t.Fatal(order, err)
	}
	run("claim")
	if msg, err := f.e.Claim("agent-a"); err != nil || msg != nil {
		t.Fatal("lease desapareceu", msg, err)
	}
	f.now = f.now.Add(time.Minute)
	msg, err := f.e.Claim("agent-a")
	if err != nil || msg == nil || msg.ExecutionID != order.CurrentExecution || msg.Fence != order.Fence || msg.MessageID != order.CurrentExecution+"-dispatch" {
		t.Fatal("intent perdido após saída abrupta", msg, err)
	}
}
