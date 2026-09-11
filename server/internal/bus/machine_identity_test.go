package bus

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
)

func TestDistributedMachineScopeAndAssignment(t *testing.T) {
	tr := newFakeTransport()
	ha, hb := hub.New(), hub.New()
	a, b := NewDistributed("a", ha, tr), NewDistributed("b", hb, tr)
	// Sem loops permanentes; apenas os handlers usados pelo cenário.
	if err := tr.Subscribe(subjPresence, a.onPresence); err != nil {
		t.Fatal(err)
	}
	if err := tr.Subscribe(subjDispatchPrefix+"b", b.onRoutedDispatch); err != nil {
		t.Fatal(err)
	}
	ag := &hub.Client{ID: "worker-b", Kind: hub.ClientAgent, Send: make(chan []byte, 4), Capabilities: []string{"COMMAND"}, StrictIdentity: true, Environment: "prod"}
	hb.Register(ag)
	defer hb.Unregister(ag)
	b.publishPresence()
	for _, tc := range []struct{ cap, env string }{{"SCRIPT", "prod"}, {"COMMAND", ""}, {"COMMAND", "dev"}} {
		if a.HasAgent(ag.ID, tc.cap, tc.env) {
			t.Fatal("presença remota ignorou escopo")
		}
		if out, _ := a.Dispatch(ag.ID, tc.cap, tc.env, []byte("denied")); out != hub.DispatchNoAgent {
			t.Fatal("pin remoto fora do escopo")
		}
	}
	assigned := false
	ag.Authorize = func() bool {
		if !assigned {
			t.Error("NATS entregou antes da atribuição")
		}
		return true
	}
	out, id := a.DispatchWithAssignment(ag.ID, "COMMAND", "prod", []byte(`{"event":"dispatch"}`), func(id string) error { assigned = id == ag.ID; return nil })
	if out != hub.DispatchSent || id != ag.ID || !assigned {
		t.Fatal("dispatch remoto não atribuiu")
	}
	select {
	case raw := <-ag.Send:
		if string(raw) != `{"event":"dispatch"}` {
			t.Fatal("payload remoto incorreto")
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch remoto não chegou")
	}
	ag.Authorize = func() bool { return false }
	a.Dispatch(ag.ID, "COMMAND", "prod", []byte(`{"event":"dispatch"}`))
	if len(ag.Send) != 0 {
		t.Fatal("nó dono entregou a credencial revogada")
	}
}
