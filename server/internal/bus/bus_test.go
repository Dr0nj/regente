package bus

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
)

// fakeTransport — pub/sub em memória, entrega síncrona, compartilhado entre nós.
// Permite testar fan-out/roteamento/presença sem um NATS real.
type fakeTransport struct {
	mu   sync.Mutex
	subs map[string][]func([]byte)
}

func newFakeTransport() *fakeTransport { return &fakeTransport{subs: map[string][]func([]byte){}} }

func (f *fakeTransport) Publish(subject string, data []byte) error {
	f.mu.Lock()
	hs := append([]func([]byte){}, f.subs[subject]...)
	f.mu.Unlock()
	cp := append([]byte{}, data...)
	for _, h := range hs {
		h(cp)
	}
	return nil
}

func (f *fakeTransport) Subscribe(subject string, handler func([]byte)) error {
	f.mu.Lock()
	f.subs[subject] = append(f.subs[subject], handler)
	f.mu.Unlock()
	return nil
}

// R5 — fan-out de eventos web: BroadcastWeb num nó chega aos web clients de outro nó.
func TestDistributed_WebFanout(t *testing.T) {
	tr := newFakeTransport()
	hubA, hubB := hub.New(), hub.New()
	a := NewDistributed("nodeA", hubA, tr)
	b := NewDistributed("nodeB", hubB, tr)
	if err := a.Start(); err != nil {
		t.Fatalf("start A: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("start B: %v", err)
	}

	wc := &hub.Client{ID: "web1", Kind: hub.ClientWeb, Send: make(chan []byte, 4)}
	hubB.Register(wc)

	a.BroadcastWeb("instance.changed", map[string]string{"id": "x", "status": "OK"})

	select {
	case raw := <-wc.Send:
		s := string(raw)
		if !strings.Contains(s, "instance.changed") || !strings.Contains(s, `"id":"x"`) {
			t.Fatalf("evento inesperado no web client do nó B: %s", s)
		}
	case <-time.After(time.Second):
		t.Fatal("web client do nó B não recebeu o evento publicado no nó A")
	}
}

// R5 — roteamento de dispatch: o líder (nó A) entrega a um agent conectado no nó B.
func TestDistributed_DispatchRoutesToOwnerNode(t *testing.T) {
	tr := newFakeTransport()
	hubA, hubB := hub.New(), hub.New()
	a := NewDistributed("nodeA", hubA, tr)
	b := NewDistributed("nodeB", hubB, tr)
	if err := a.Start(); err != nil {
		t.Fatalf("start A: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("start B: %v", err)
	}

	ag := &hub.Client{ID: "agB", Kind: hub.ClientAgent, Send: make(chan []byte, 4), Capabilities: []string{"COMMAND"}}
	hubB.Register(ag)
	b.publishPresence() // propaga a presença de B para A

	out, id := a.Dispatch("", "COMMAND", "", []byte(`{"event":"dispatch","jobType":"COMMAND"}`))
	if out != hub.DispatchSent || id != "agB" {
		t.Fatalf("esperava DispatchSent p/ agB, veio out=%v id=%q", out, id)
	}
	select {
	case raw := <-ag.Send:
		if !strings.Contains(string(raw), "dispatch") {
			t.Fatalf("payload inesperado no agent do nó B: %s", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("agent do nó B não recebeu o dispatch roteado do nó A")
	}
}

// Sem presença remota nem agent local, Dispatch reporta NoAgent (cai no mock).
func TestDistributed_DispatchNoAgent(t *testing.T) {
	tr := newFakeTransport()
	a := NewDistributed("nodeA", hub.New(), tr)
	if err := a.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if out, _ := a.Dispatch("", "COMMAND", "", []byte(`{}`)); out != hub.DispatchNoAgent {
		t.Fatalf("esperava DispatchNoAgent, veio %v", out)
	}
}

// ADV-2 — roteamento por ambiente cruza nós: a presença carrega o env do agente
// e o Dispatch com env exigente NÃO cai num agente de outro ambiente.
func TestDistributed_DispatchEnvRouting(t *testing.T) {
	tr := newFakeTransport()
	hubA, hubB := hub.New(), hub.New()
	a := NewDistributed("nodeA", hubA, tr)
	b := NewDistributed("nodeB", hubB, tr)
	if err := a.Start(); err != nil {
		t.Fatalf("start A: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("start B: %v", err)
	}

	ag := &hub.Client{ID: "agProd", Kind: hub.ClientAgent, Send: make(chan []byte, 4),
		Capabilities: []string{"COMMAND"}, Environment: "prod"}
	hubB.Register(ag)
	b.publishPresence()

	// Job de OUTRO ambiente não roteia pro agente prod…
	if out, _ := a.Dispatch("", "COMMAND", "dev", []byte(`{}`)); out != hub.DispatchNoAgent {
		t.Fatalf("job dev não podia cair no agente prod, veio %v", out)
	}
	if a.HasAgent("", "COMMAND", "dev") {
		t.Fatal("HasAgent(dev) não podia contar o agente prod remoto")
	}
	// …mas o mesmo ambiente (e o job sem env) roteiam.
	if out, id := a.Dispatch("", "COMMAND", "PROD", []byte(`{}`)); out != hub.DispatchSent || id != "agProd" {
		t.Fatalf("job prod deveria rotear (case-insensitive), veio out=%v id=%q", out, id)
	}
	if out, _ := a.Dispatch("", "COMMAND", "", []byte(`{}`)); out != hub.DispatchSent {
		t.Fatal("job sem env deveria aceitar qualquer agente")
	}
}
