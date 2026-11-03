package hub

import "testing"

func TestAgentIdentityRoutingIgnoresLegacyEnvironment(t *testing.T) {
	h := New()
	a := &Client{ID: "prod-worker", Kind: ClientAgent, Send: make(chan []byte, 8), StrictIdentity: true, Environment: "prod", Capabilities: []string{"COMMAND"}}
	h.Register(a)
	defer h.Unregister(a)
	for _, env := range []string{"", "local", "prod", "qa"} {
		if !EnvMatch(env, "prod") || !h.HasAgent(a.ID, "COMMAND", env) {
			t.Fatalf("legacy environment blocked pinned agent: %q", env)
		}
		if out, id := h.Dispatch(a.ID, "COMMAND", env, []byte("test")); out != DispatchSent || id != a.ID {
			t.Fatalf("dispatch failed: %q %v", env, out)
		}
		<-a.Send
		if h.HasAgent("other-agent", "COMMAND", env) || h.HasAgent(a.ID, "SCRIPT", env) {
			t.Fatal("identity or capability restriction was lost")
		}
	}
}
