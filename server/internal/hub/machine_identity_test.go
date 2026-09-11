package hub

import (
	"errors"
	"testing"
)

func TestMachineDispatchScopeAssignmentAndReplacement(t *testing.T) {
	h := New()
	a := &Client{ID: "a", Kind: ClientAgent, Send: make(chan []byte, 4), Capabilities: []string{"COMMAND"}, StrictIdentity: true, Environment: "prod"}
	h.Register(a)
	defer h.Unregister(a)
	for _, tc := range []struct{ cap, env string }{{"SCRIPT", "prod"}, {"COMMAND", ""}, {"COMMAND", "dev"}} {
		if h.HasAgent("a", tc.cap, tc.env) {
			t.Fatal("pin ignorou escopo", tc)
		}
		if out, _ := h.Dispatch("a", tc.cap, tc.env, []byte("denied")); out != DispatchNoAgent {
			t.Fatal("dispatch fora do escopo", tc)
		}
	}
	if out, _ := h.DispatchWithAssignment("a", "COMMAND", "prod", []byte("denied"), func(string) error { return errors.New("db unavailable") }); out != DispatchNoAgent {
		t.Fatal("falha de atribuição entregou payload")
	}
	if len(a.Send) != 0 {
		t.Fatal("canal recebeu payload negado")
	}
	assigned := false
	if out, _ := h.DispatchWithAssignment("a", "COMMAND", "prod", []byte("ok"), func(id string) error {
		if id != "a" || len(a.Send) != 0 {
			t.Fatal("atribuição ocorreu após entrega")
		}
		assigned = true
		return nil
	}); out != DispatchSent || !assigned {
		t.Fatal("atribuição não precedeu entrega")
	}
	<-a.Send
	a.Authorize = func() bool { return false }
	if out, _ := h.Dispatch("a", "COMMAND", "prod", []byte("revoked")); out != DispatchNoAgent {
		t.Fatal("credencial inválida recebeu dispatch")
	}
	b := &Client{ID: "a", Kind: ClientAgent, Send: make(chan []byte, 1)}
	h.Register(b)
	defer h.Unregister(b)
	h.Unregister(a)
	if h.GetAgent("a") != b {
		t.Fatal("desconexão antiga removeu a sessão nova")
	}
	select {
	case <-a.Done:
	default:
		t.Fatal("sessão substituída não foi encerrada")
	}
}
