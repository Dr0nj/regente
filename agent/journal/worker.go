package journal

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type TransportError struct{ Err error }

func (e *TransportError) Error() string { return e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }
func transportError(err error) error {
	if err == nil {
		return nil
	}
	return &TransportError{err}
}

type Receipt struct {
	Reconciled      bool   `json:"reconciled"`
	Accepted        bool   `json:"accepted"`
	Duplicate       bool   `json:"duplicate"`
	ExecutionID     string `json:"executionId"`
	State           string `json:"state"`
	Current         bool   `json:"current"`
	StartAuthorized bool   `json:"startAuthorized"`
}
type Identity struct {
	Protocol    int    `json:"protocol"`
	ExecutionID string `json:"executionId"`
	Fence       int64  `json:"fence"`
}
type Transport interface {
	Poll(context.Context, bool) (*Envelope, error)
	Post(context.Context, string, any) (Receipt, error)
}
type Executor func(context.Context, Envelope, func(string)) (int, string)
type Worker struct {
	Journal    *Journal
	Transport  Transport
	Execute    Executor
	Concurrent int
	mu         sync.Mutex
	active     map[string]context.CancelFunc
	heartbeat  map[string]time.Time
	wg         sync.WaitGroup
	fatalErr   error
}

func NewWorker(j *Journal, t Transport, exec Executor, n int) *Worker {
	return &Worker{Journal: j, Transport: t, Execute: exec, Concurrent: n, active: map[string]context.CancelFunc{}, heartbeat: map[string]time.Time{}}
}
func (w *Worker) identity(e Entry) Identity {
	return Identity{2, e.Envelope.ExecutionID, e.Envelope.Fence}
}
func (w *Worker) phase(ctx context.Context, e Entry, kind string) (Receipt, error) {
	id := w.identity(e)
	r, err := w.Transport.Post(ctx, "ack", struct {
		Identity
		Kind   string `json:"kind"`
		Reason string `json:"reason,omitempty"`
	}{id, kind, func() string {
		if kind == "uncertain" {
			return e.Reason
		}
		return ""
	}()})
	return r, transportError(err)
}
func terminal(s string) bool           { return s == "succeeded" || s == "failed" || s == "cancelled" }
func received(r Receipt, e Entry) bool { return r.Accepted && r.ExecutionID == e.Envelope.ExecutionID }
func (w *Worker) Sync(ctx context.Context, e Entry) error {
	j := w.Journal
	switch e.State {
	case "uncertain":
		r, err := w.phase(ctx, e, "uncertain")
		if err != nil {
			return err
		}
		if !received(r, e) {
			return errors.New("invalid durable receipt")
		}
		if terminal(r.State) {
			return j.Ack(e.Envelope.ExecutionID)
		}
		// Sysout persistido permanece legível mesmo quando o resultado é incerto.
	case "accepted", "starting":
		w.mu.Lock()
		available := len(w.active) < w.Concurrent
		w.mu.Unlock()
		if !available {
			return nil
		}
		if !e.AcceptedAck {
			r, err := w.phase(ctx, e, "accepted")
			if err != nil {
				return err
			}
			if !received(r, e) {
				return errors.New("invalid acceptance receipt")
			}
			if r.State == "cancel_requested" {
				return j.CancelBeforeEffect(e.Envelope.ExecutionID)
			}
			if terminal(r.State) || !r.Current || r.State == "uncertain" {
				return j.Uncertain(e.Envelope.ExecutionID, "server no longer authorizes this execution")
			}
			if err = j.AckPhase(e.Envelope.ExecutionID, "accepted"); err != nil {
				return err
			}
		}
		if e.State == "accepted" {
			ok, err := j.BeginEffect(e.Envelope.ExecutionID)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		// Marcador starting já durável: perda de processo aqui não autoriza reexecutar no restart.
		r, err := w.phase(ctx, e, "started")
		if err != nil {
			return err
		}
		if r.State == "cancel_requested" {
			return j.CancelBeforeEffect(e.Envelope.ExecutionID)
		}
		if !received(r, e) || !r.StartAuthorized {
			return j.Uncertain(e.Envelope.ExecutionID, "server refused start authorization")
		}
		if err = j.AckPhase(e.Envelope.ExecutionID, "started"); err != nil {
			return err
		}
		if err = j.Running(e.Envelope.ExecutionID); err != nil {
			return err
		}
		w.launch(ctx, e)
		return nil
	case "running":
		w.mu.Lock()
		last := w.heartbeat[e.Envelope.ExecutionID]
		w.mu.Unlock()
		if time.Since(last) > 30*time.Second {
			r, err := w.phase(ctx, e, "heartbeat")
			if err != nil {
				return err
			}
			if !received(r, e) {
				return errors.New("invalid heartbeat receipt")
			}
			w.mu.Lock()
			w.heartbeat[e.Envelope.ExecutionID] = time.Now()
			w.mu.Unlock()
		}
	case "result_pending", "cancel_pending":
		r, err := w.phase(ctx, e, "status")
		if err != nil {
			return err
		}
		if !received(r, e) {
			return errors.New("invalid reconciliation receipt")
		}
		if r.Reconciled && terminal(r.State) {
			return j.Ack(e.Envelope.ExecutionID)
		}
	default:
		return fmt.Errorf("unsupported journal state %q", e.State)
	}
	// Chunks são enviados em sequência e só apagados após receipt do mesmo executionId.
	if e.StartedAck {
		chunks, err := j.Chunks(e.Envelope.ExecutionID)
		if err != nil {
			return err
		}
		for _, c := range chunks {
			r, err := w.Transport.Post(ctx, "output", struct {
				Identity
				Seq   int64  `json:"seq"`
				Chunk string `json:"chunk"`
			}{w.identity(e), c.Seq, c.Text})
			if err != nil {
				return transportError(err)
			}
			if !received(r, e) {
				return errors.New("invalid output receipt")
			}
			if err = j.AckChunk(e.Envelope.ExecutionID, c.Seq); err != nil {
				return err
			}
		}
		remaining, err := j.Chunks(e.Envelope.ExecutionID)
		if err != nil {
			return err
		}
		if len(remaining) > 0 {
			return nil
		}
	}
	if e.State == "result_pending" {
		r, err := w.Transport.Post(ctx, "result", struct {
			Identity
			ExitCode int    `json:"exitCode"`
			Output   string `json:"output"`
		}{w.identity(e), e.ExitCode, e.Output})
		if err != nil {
			return transportError(err)
		}
		if !received(r, e) {
			return errors.New("invalid result receipt")
		}
		return j.Ack(e.Envelope.ExecutionID)
	}
	if e.State == "cancel_pending" {
		r, err := w.phase(ctx, e, "cancelled")
		if err != nil {
			return err
		}
		if !received(r, e) {
			return errors.New("invalid cancellation receipt")
		}
		return j.Ack(e.Envelope.ExecutionID)
	}
	return nil
}
func (w *Worker) launch(parent context.Context, e Entry) {
	w.mu.Lock()
	if _, ok := w.active[e.Envelope.ExecutionID]; ok {
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.active[e.Envelope.ExecutionID] = cancel
	w.heartbeat[e.Envelope.ExecutionID] = time.Now()
	w.mu.Unlock()
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer cancel()
		defer func() { w.mu.Lock(); delete(w.active, e.Envelope.ExecutionID); w.mu.Unlock() }()
		defer func() {
			if r := recover(); r != nil {
				if err := w.Journal.Uncertain(e.Envelope.ExecutionID, "executor panicked; effect outcome unknown"); err != nil {
					w.fail(err)
				}
			}
		}()
		fresh, err := w.Journal.Entry(e.Envelope.ExecutionID)
		if err != nil {
			w.fail(err)
			return
		}
		if fresh.CancelRequested {
			cancel()
		}
		var writeErr error
		var emitMu sync.Mutex
		emit := func(text string) {
			emitMu.Lock()
			defer emitMu.Unlock()
			if writeErr != nil {
				return
			}
			err := w.Journal.Append(e.Envelope.ExecutionID, text)
			if errors.Is(err, ErrOutputLimit) {
				return
			} // cap persistido; resultado final também é limitado.
			if err != nil {
				writeErr = err
				cancel()
				w.fail(err)
			}
		}
		ctx = WithProcessRecorder(ctx, func(pid int) error {
			err := w.Journal.Process(e.Envelope.ExecutionID, pid)
			if err != nil {
				emitMu.Lock()
				writeErr = err
				emitMu.Unlock()
				cancel()
				w.fail(err)
			}
			return err
		})
		code, out := w.Execute(ctx, e.Envelope, emit)
		if code == UnknownExitCode {
			latest, readErr := w.Journal.Entry(e.Envelope.ExecutionID)
			if readErr != nil {
				w.fail(readErr)
				return
			}
			if !latest.CancelRequested {
				if err := w.Journal.Uncertain(e.Envelope.ExecutionID, out); err != nil {
					w.fail(err)
				}
				return
			}
			code = -1
		}
		emitMu.Lock()
		err = writeErr
		emitMu.Unlock()
		if err != nil {
			return
		}
		if err = w.Journal.Result(e.Envelope.ExecutionID, code, out); err != nil {
			w.fail(err)
		}
	}()
}
func (w *Worker) fail(err error) {
	w.mu.Lock()
	if w.fatalErr == nil {
		w.fatalErr = err
	}
	w.mu.Unlock()
}
func (w *Worker) Tick(ctx context.Context) error {
	w.mu.Lock()
	fatal := w.fatalErr
	w.mu.Unlock()
	if fatal != nil {
		return fmt.Errorf("journal persistence failed; new effects stopped: %w", fatal)
	}
	entries, err := w.Journal.Pending()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err = w.Sync(ctx, e); err != nil {
			return err
		}
	}
	entries, err = w.Journal.Pending()
	if err != nil {
		return err
	}
	message, err := w.Transport.Poll(ctx, len(entries) < w.Journal.MaxPending)
	if err != nil {
		return transportError(err)
	}
	if message != nil {
		e, err := w.Journal.Accept(*message)
		if err != nil {
			return err
		}
		if message.Kind == "cancel" {
			w.mu.Lock()
			c := w.active[e.Envelope.ExecutionID]
			w.mu.Unlock()
			if c != nil {
				c()
			}
		}
	}
	return nil
}
func (w *Worker) Stop() {
	w.mu.Lock()
	for _, c := range w.active {
		c()
	}
	w.mu.Unlock()
	w.wg.Wait()
}
