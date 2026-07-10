package hub

import "testing"

// ADV-2 — roteamento por ambiente no hub local: EnvMatch (coringa dos dois
// lados, match case-insensitive) e o efeito em PickAgent/HasAgent/Dispatch,
// inclusive agente PINADO em ambiente conflitante.

func TestEnvMatch(t *testing.T) {
	cases := []struct {
		defEnv, agentEnv string
		want             bool
	}{
		{"", "", true},
		{"", "prod", true},     // job sem env roda em qualquer agente
		{"prod", "", true},     // agente sem label é generalista
		{"prod", "prod", true},
		{"PROD", "prod", true}, // case-insensitive
		{"prod", "dev", false},
	}
	for _, c := range cases {
		if got := EnvMatch(c.defEnv, c.agentEnv); got != c.want {
			t.Errorf("EnvMatch(%q,%q)=%v, esperado %v", c.defEnv, c.agentEnv, got, c.want)
		}
	}
}

func TestHubEnvRouting(t *testing.T) {
	h := New()
	prod := &Client{ID: "ag-prod", Kind: ClientAgent, Send: make(chan []byte, 4),
		Capabilities: []string{"COMMAND"}, Environment: "prod"}
	h.Register(prod)

	// PickAgent respeita o env
	if a := h.PickAgent("COMMAND", "dev"); a != nil {
		t.Fatalf("PickAgent(dev) não podia devolver o agente prod, veio %s", a.ID)
	}
	if a := h.PickAgent("COMMAND", "prod"); a == nil || a.ID != "ag-prod" {
		t.Fatal("PickAgent(prod) deveria achar ag-prod")
	}
	if a := h.PickAgent("COMMAND", ""); a == nil {
		t.Fatal("PickAgent sem env deveria aceitar qualquer agente")
	}

	// HasAgent por capability e por pin
	if h.HasAgent("", "COMMAND", "dev") {
		t.Fatal("HasAgent(dev) não podia contar o agente prod")
	}
	if !h.HasAgent("", "COMMAND", "prod") {
		t.Fatal("HasAgent(prod) deveria contar ag-prod")
	}
	// Pinado em env conflitante = misconfiguração → NÃO conta (fica WAIT_AGENT
	// com o motivo no Explain, em vez de rodar job dev no agente prod).
	if h.HasAgent("ag-prod", "COMMAND", "dev") {
		t.Fatal("pin em agente de outro env não podia contar")
	}
	if !h.HasAgent("ag-prod", "COMMAND", "prod") {
		t.Fatal("pin no env certo deveria contar")
	}
	if !h.HasAgent("ag-prod", "COMMAND", "") {
		t.Fatal("pin sem env no job deveria contar")
	}

	// Dispatch segue a mesma regra
	if out, _ := h.Dispatch("", "COMMAND", "dev", []byte(`{}`)); out != DispatchNoAgent {
		t.Fatalf("dispatch dev não podia entregar no agente prod, veio %v", out)
	}
	if out, id := h.Dispatch("", "COMMAND", "prod", []byte(`{}`)); out != DispatchSent || id != "ag-prod" {
		t.Fatalf("dispatch prod deveria entregar em ag-prod, veio out=%v id=%q", out, id)
	}
	if out, _ := h.Dispatch("ag-prod", "COMMAND", "dev", []byte(`{}`)); out != DispatchNoAgent {
		t.Fatalf("dispatch pinado em env conflitante não podia entregar, veio %v", out)
	}

	// Agente generalista (sem env) serve job com env
	generic := &Client{ID: "ag-any", Kind: ClientAgent, Send: make(chan []byte, 4),
		Capabilities: []string{"SCRIPT"}}
	h.Register(generic)
	if a := h.PickAgent("SCRIPT", "prod"); a == nil || a.ID != "ag-any" {
		t.Fatal("agente sem label deveria servir job com env (generalista)")
	}
}
