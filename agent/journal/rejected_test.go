package journal

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// rejectingTransport recusa (HTTP 409) tudo que diz respeito a UMA execução e
// atende as outras normalmente.
type rejectingTransport struct {
	mu       sync.Mutex
	poison   string
	accepted map[string]int
}

func (r *rejectingTransport) Poll(context.Context, bool) (*Envelope, error) { return nil, nil }
func (r *rejectingTransport) Post(_ context.Context, path string, body any) (Receipt, error) {
	raw, _ := json.Marshal(body)
	var id struct {
		ExecutionID string `json:"executionId"`
	}
	_ = json.Unmarshal(raw, &id)
	if id.ExecutionID == r.poison {
		return Receipt{}, &RejectedError{Path: path, Status: 409}
	}
	r.mu.Lock()
	r.accepted[id.ExecutionID]++
	r.mu.Unlock()
	return Receipt{Accepted: true, ExecutionID: id.ExecutionID, State: "running", Current: true, StartAuthorized: true}, nil
}

// Regressão da VPS (14/11/2026): uma entrada recusada pelo server abortava o
// tick inteiro, e as execuções seguintes ficavam sem ack/heartbeat até o lease
// vencer. A recusa continua sendo reportada, mas não segura as outras.
func TestRejectedEntryDoesNotBlockTheJournal(t *testing.T) {
	j, _ := openTest(t)
	accept(t, j, "poison")
	if err := j.Uncertain("poison", "server no longer authorizes this execution"); err != nil {
		t.Fatal(err)
	}
	accept(t, j, "healthy")
	var effects atomic.Int32
	done := make(chan struct{})
	tr := &rejectingTransport{poison: "poison", accepted: map[string]int{}}
	w := NewWorker(j, tr, func(context.Context, Envelope, func(string)) (int, string) {
		if effects.Add(1) == 1 {
			close(done)
		}
		return 0, "ok"
	}, 2)
	defer w.Stop()
	err := w.Tick(context.Background())
	var transport *TransportError
	var rejected *RejectedError
	if !errors.As(err, &transport) || !errors.As(err, &rejected) || rejected.Status != 409 {
		t.Fatal("the refusal must still be reported as a transport error", err)
	}
	<-done
	tr.mu.Lock()
	served := tr.accepted["healthy"]
	tr.mu.Unlock()
	if served == 0 || effects.Load() != 1 {
		t.Fatal("healthy execution was held hostage by the rejected one", served, effects.Load())
	}
	if e, _ := j.Entry("poison"); e.State != "uncertain" {
		t.Fatal("rejected entry must stay in the journal", e.State)
	}
}

// Rede caída NÃO é recusa: aborta o tick como antes, sem martelar cada entrada.
func TestNetworkFailureStillAbortsTheTick(t *testing.T) {
	j, _ := openTest(t)
	accept(t, j, "one")
	f := &fakeTransport{failure: "ack"}
	w := NewWorker(j, f, func(context.Context, Envelope, func(string)) (int, string) { return 0, "" }, 1)
	defer w.Stop()
	err := w.Tick(context.Background())
	var rejected *RejectedError
	if err == nil || errors.As(err, &rejected) {
		t.Fatal("network failure must abort and must not be classified as a refusal", err)
	}
	f.mu.Lock()
	posts := len(f.posts)
	f.mu.Unlock()
	if posts != 1 {
		t.Fatal("tick kept going after a network failure", posts)
	}
}
