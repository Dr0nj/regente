package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func message(id string) Envelope {
	return Envelope{2, "dispatch", id + "-dispatch", id, "order-" + id, 1, "worker", 1, id, []byte("{}")}
}
func openTest(t *testing.T) (*Journal, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.db")
	j, err := Open(path, "worker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j, path
}
func accept(t *testing.T, j *Journal, id string) Entry {
	t.Helper()
	e, err := j.Accept(message(id))
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func started(t *testing.T, j *Journal, id string) {
	t.Helper()
	accept(t, j, id)
	if err := j.AckPhase(id, "accepted"); err != nil {
		t.Fatal(err)
	}
	if ok, err := j.BeginEffect(id); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := j.AckPhase(id, "started"); err != nil {
		t.Fatal(err)
	}
	if err := j.Running(id); err != nil {
		t.Fatal(err)
	}
}

func TestI09JournalRestartBoundaries(t *testing.T) {
	j, path := openTest(t)
	accept(t, j, "accepted")
	accept(t, j, "starting")
	j.AckPhase("starting", "accepted")
	j.BeginEffect("starting")
	started(t, j, "running")
	started(t, j, "result")
	j.Append("result", "output")
	j.Result("result", 0, "output")
	j.Close()
	j, err := Open(path, "worker")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	for id, state := range map[string]string{"accepted": "accepted", "starting": "uncertain", "running": "uncertain", "result": "result_pending"} {
		e, err := j.Entry(id)
		if err != nil || e.State != state {
			t.Fatalf("%s: %#v %v", id, e, err)
		}
	}
	chunks, err := j.Chunks("result")
	if err != nil || len(chunks) != 1 || chunks[0].Seq != 1 {
		t.Fatal(chunks, err)
	}
}
func TestI09JournalIdentityCapacityAndCancel(t *testing.T) {
	j, _ := openTest(t)
	j.MaxPending = 1
	accept(t, j, "one")
	if _, err := j.Accept(message("one")); err != nil {
		t.Fatal(err)
	}
	altered := message("one")
	altered.Fence++
	if _, err := j.Accept(altered); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	altered = message("one")
	altered.Definition = []byte("null")
	if _, err := j.Accept(altered); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := j.Accept(message("two")); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	cancel := message("one")
	cancel.Kind = "cancel"
	cancel.MessageID = "one-cancel"
	cancel.Definition = nil
	e, err := j.Accept(cancel)
	if err != nil || e.State != "cancel_pending" {
		t.Fatal(e, err)
	}
	cancel = message("never-dispatched")
	cancel.Kind = "cancel"
	cancel.Definition = nil
	if _, err = j.Accept(cancel); err != nil {
		t.Fatal(err)
	}
	if _, err = j.Accept(message("never-dispatched")); err != nil {
		t.Fatal(err)
	}
	e, _ = j.Entry("never-dispatched")
	if e.State != "cancel_pending" {
		t.Fatal(e.State)
	}
}
func TestI09JournalLockIdentityVersionCorruption(t *testing.T) {
	j, path := openTest(t)
	if second, err := Open(path, "worker"); err == nil {
		second.Close()
		t.Fatal("concurrent owner admitted")
	}
	j.Close()
	if second, err := Open(path, "other"); err == nil {
		second.Close()
		t.Fatal("different identity admitted")
	}
	j, err := Open(path, "worker")
	if err != nil {
		t.Fatal(err)
	}
	j.DB.Exec("UPDATE journal_meta SET version=999")
	j.Close()
	if second, err := Open(path, "worker"); err == nil {
		second.Close()
		t.Fatal("unknown version admitted")
	}
	bad := filepath.Join(t.TempDir(), "corrupt.db")
	os.WriteFile(bad, []byte("not a database"), 0600)
	if second, err := Open(bad, "worker"); err == nil {
		second.Close()
		t.Fatal("corrupt journal admitted")
	}
}
func TestI09JournalStorageFailureBeforeAcceptance(t *testing.T) {
	j, _ := openTest(t)
	if _, err := j.DB.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Accept(message("denied")); err == nil {
		t.Fatal("read-only storage accepted dispatch")
	}
	if _, err := j.Entry("denied"); err == nil {
		t.Fatal("phantom acceptance")
	}
}
func TestI09JournalCompactOnlyConfirmed(t *testing.T) {
	j, _ := openTest(t)
	started(t, j, "confirmed")
	j.Append("confirmed", "chunk")
	j.Result("confirmed", 0, "final")
	j.Ack("confirmed")
	started(t, j, "unconfirmed")
	j.Append("unconfirmed", "chunk")
	j.Result("unconfirmed", 0, "final")
	if err := j.Compact(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	c, _ := j.Entry("confirmed")
	u, _ := j.Entry("unconfirmed")
	chunks, _ := j.Chunks("unconfirmed")
	if c.State != "acked" || c.Output != "" || len(c.Envelope.Definition) != 0 || u.Output != "final" || len(chunks) != 1 {
		t.Fatal(c, u, chunks)
	}
	if _, err := j.Accept(message("confirmed")); err != nil {
		t.Fatal(err)
	}
	altered := message("confirmed")
	altered.Definition = []byte("null")
	if _, err := j.Accept(altered); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

type fakeTransport struct {
	mu         sync.Mutex
	messages   []Envelope
	posts      []string
	failure    string
	state      string
	authorized bool
	available  []bool
}

func (f *fakeTransport) Poll(_ context.Context, available bool) (*Envelope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.available = append(f.available, available)
	if len(f.messages) == 0 {
		return nil, nil
	}
	m := f.messages[0]
	f.messages = f.messages[1:]
	return &m, nil
}
func (f *fakeTransport) Post(_ context.Context, path string, body any) (Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, path)
	if f.failure == path {
		return Receipt{}, errors.New("receipt lost")
	}
	state := f.state
	if state == "" {
		state = "running"
	}
	return Receipt{Accepted: true, ExecutionID: "one", State: state, Current: true, StartAuthorized: f.authorized}, nil
}
func waitEntry(t *testing.T, j *Journal, id, state string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e, err := j.Entry(id)
		if err == nil && e.State == state {
			return
		}
		time.Sleep(time.Millisecond)
	}
	e, err := j.Entry(id)
	t.Fatal("state", e.State, err, "expected", state)
}
func TestI09WorkerLostResultReceiptRestartNoRepeatedEffect(t *testing.T) {
	j, path := openTest(t)
	accept(t, j, "one")
	transport := &fakeTransport{authorized: true, failure: "result"}
	var effects atomic.Int32
	exec := func(context.Context, Envelope, func(string)) (int, string) {
		effects.Add(1)
		return 0, "committed effect"
	}
	w := NewWorker(j, transport, exec, 1)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitEntry(t, j, "one", "result_pending")
	w.Stop()
	if err := w.Tick(context.Background()); err == nil {
		t.Fatal("expected lost receipt")
	}
	j.Close()
	j, err := Open(path, "worker")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	transport.failure = ""
	w = NewWorker(j, transport, exec, 1)
	defer w.Stop()
	if err = w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	e, _ := j.Entry("one")
	if e.State != "acked" || effects.Load() != 1 {
		t.Fatal(e, effects.Load())
	}
}
func TestI09WorkerStartedRestartNeverReplays(t *testing.T) {
	j, path := openTest(t)
	started(t, j, "one")
	if err := j.Append("one", "diagnostic persisted before crash"); err != nil {
		t.Fatal(err)
	}
	j.Close()
	j, err := Open(path, "worker")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var effects atomic.Int32
	f := &fakeTransport{authorized: true, state: "uncertain"}
	w := NewWorker(j, f, func(context.Context, Envelope, func(string)) (int, string) { effects.Add(1); return 0, "" }, 1)
	defer w.Stop()
	for range 3 {
		if err = w.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(strings.Join(f.posts, ","), "output") != 1 {
		t.Fatal("uncertain sysout not sent exactly once", f.posts)
	}
	if effects.Load() != 0 {
		t.Fatal("effect replayed")
	}
	e, _ := j.Entry("one")
	if e.State != "uncertain" {
		t.Fatal(e)
	}
}
func TestI09WorkerStaleStartReceiptNeverExecutes(t *testing.T) {
	j, _ := openTest(t)
	accept(t, j, "one")
	var effects atomic.Int32
	f := &fakeTransport{authorized: false}
	w := NewWorker(j, f, func(context.Context, Envelope, func(string)) (int, string) { effects.Add(1); return 0, "" }, 1)
	defer w.Stop()
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if effects.Load() != 0 {
		t.Fatal("stale ACK launched an effect")
	}
	e, _ := j.Entry("one")
	if e.State != "uncertain" {
		t.Fatal(e)
	}
}
func TestI09WorkerDiskFailureAfterEffectStopsAdmission(t *testing.T) {
	j, _ := openTest(t)
	accept(t, j, "one")
	f := &fakeTransport{authorized: true}
	w := NewWorker(j, f, func(_ context.Context, _ Envelope, emit func(string)) (int, string) {
		j.DB.Exec("CREATE TRIGGER disk_full BEFORE INSERT ON journal_chunks BEGIN SELECT RAISE(FAIL,'database or disk is full'); END")
		emit("output")
		return 0, "external effect completed"
	}, 1)
	defer w.Stop()
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.wg.Wait()
	if err := w.Tick(context.Background()); err == nil {
		t.Fatal("storage failure did not stop admission")
	} else {
		var net *TransportError
		if errors.As(err, &net) {
			t.Fatal("storage failure classified as network")
		}
	}
	j.DB.Exec("DROP TRIGGER disk_full")
	if err := w.Tick(context.Background()); err == nil {
		t.Fatal("fatal state cleared")
	}
	e, _ := j.Entry("one")
	if e.State != "running" {
		t.Fatal(e)
	}
}
func TestI09WorkerConcurrencyAndAdmission(t *testing.T) {
	j, _ := openTest(t)
	j.MaxPending = 2
	accept(t, j, "one")
	accept(t, j, "two")
	f := &fakeTransport{authorized: true}
	gate := make(chan struct{})
	entered := make(chan struct{}, 2)
	w := NewWorker(j, f, func(context.Context, Envelope, func(string)) (int, string) {
		entered <- struct{}{}
		<-gate
		return 0, ""
	}, 1)
	defer func() { close(gate); w.Stop() }()
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entered) != 0 {
		t.Fatal("concurrency exceeded")
	}
	if len(f.available) != 2 || f.available[0] || f.available[1] {
		t.Fatal("admission exceeded", f.available)
	}
	e, _ := j.Entry("two")
	if e.State != "accepted" {
		t.Fatal(e)
	}
}
func TestI09JournalOutputLimit(t *testing.T) {
	j, _ := openTest(t)
	started(t, j, "one")
	if err := j.Append("one", strings.Repeat("x", MaxOutput)); err != nil {
		t.Fatal(err)
	}
	if err := j.Append("one", "x"); !errors.Is(err, ErrOutputLimit) {
		t.Fatal(err)
	}
}
