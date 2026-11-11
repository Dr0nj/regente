package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

type batchTransport struct {
	messages []Envelope
	polls    int
	fail     bool
	posts    int
}

func (b *batchTransport) Poll(_ context.Context, available bool) (*Envelope, error) {
	b.polls++
	if !available || len(b.messages) == 0 {
		return nil, nil
	}
	e := b.messages[0]
	b.messages = b.messages[1:]
	return &e, nil
}
func (b *batchTransport) Post(_ context.Context, _ string, body any) (Receipt, error) {
	b.posts++
	if b.fail {
		return Receipt{}, errors.New("receipt unavailable")
	}
	raw, _ := json.Marshal(body)
	var id Identity
	_ = json.Unmarshal(raw, &id)
	return Receipt{Accepted: true, ExecutionID: id.ExecutionID, Current: true, StartAuthorized: true, State: "running"}, nil
}
func TestBatchAcceptsAndAuthorizesWithoutAnotherTick(t *testing.T) {
	j, _ := openTest(t)
	f := &batchTransport{messages: []Envelope{message("one"), message("two"), message("three")}}
	entered := make(chan string, 3)
	release := make(chan struct{})
	w := NewWorker(j, f, func(_ context.Context, e Envelope, _ func(string)) (int, string) {
		entered <- e.ExecutionID
		<-release
		return 0, ""
	}, 3)
	defer func() { close(release); w.Stop() }()
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "three"} {
		e, err := j.Entry(id)
		if err != nil || !e.AcceptedAck || !e.StartedAck || e.State != "running" {
			t.Fatalf("autorização não concluída no mesmo tick: %+v %v", e, err)
		}
	}
	if f.polls != 4 || f.posts != 6 {
		t.Fatalf("lote inesperado: polls=%d posts=%d", f.polls, f.posts)
	}
}
func TestBatchStopsOnLostReceiptAndRetainsJournal(t *testing.T) {
	j, _ := openTest(t)
	f := &batchTransport{messages: []Envelope{message("one"), message("two")}, fail: true}
	var effects atomic.Int32
	w := NewWorker(j, f, func(context.Context, Envelope, func(string)) (int, string) { effects.Add(1); return 0, "" }, 2)
	defer w.Stop()
	var transport *TransportError
	if err := w.Tick(context.Background()); !errors.As(err, &transport) {
		t.Fatalf("esperava falha de transporte: %v", err)
	}
	e, err := j.Entry("one")
	if err != nil || e.State != "accepted" || e.AcceptedAck || effects.Load() != 0 || len(f.messages) != 1 {
		t.Fatalf("perda de recibo não preservada: %+v effects=%d remaining=%d", e, effects.Load(), len(f.messages))
	}
}
func TestBatchBoundAndJournalCapacity(t *testing.T) {
	for _, limit := range []int{2, 1000} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			j, _ := openTest(t)
			j.MaxPending = limit
			f := &batchTransport{}
			for i := 0; i < 40; i++ {
				f.messages = append(f.messages, message(fmt.Sprintf("job-%02d", i)))
			}
			release := make(chan struct{})
			w := NewWorker(j, f, func(context.Context, Envelope, func(string)) (int, string) { <-release; return 0, "" }, 1)
			defer func() { close(release); w.Stop() }()
			if err := w.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			pending, err := j.Pending()
			if err != nil {
				t.Fatal(err)
			}
			want := min(limit, 32)
			if len(pending) != want || len(f.messages) != 40-want {
				t.Fatalf("limite ignorado: pending=%d restam=%d", len(pending), len(f.messages))
			}
		})
	}
}
func TestPendingIndexExcludesDurableTombstones(t *testing.T) {
	j, path := openTest(t)
	accept(t, j, "old")
	if err := j.Ack("old"); err != nil {
		t.Fatal(err)
	}
	accept(t, j, "new")
	rows, err := j.DB.Query("EXPLAIN QUERY PLAN SELECT execution_id FROM journal_entries WHERE state!='acked' ORDER BY execution_id LIMIT 1001")
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var a, b, c int
		var detail string
		if err = rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
	}
	rows.Close()
	if !strings.Contains(plan.String(), "journal_pending_idx") {
		t.Fatalf("fila voltou a varrer histórico: %s", plan.String())
	}
	pending, err := j.Pending()
	if err != nil || len(pending) != 1 || pending[0].Envelope.ExecutionID != "new" {
		t.Fatalf("fila incorreta: %+v %v", pending, err)
	}
	j.Close()
	again, err := Open(path, "worker")
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	entry, err := again.Accept(message("old"))
	if err != nil || entry.State != "acked" {
		t.Fatalf("tombstone perdido após reabertura: %+v %v", entry, err)
	}
}
