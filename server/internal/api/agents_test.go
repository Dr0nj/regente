package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/bus"
	"github.com/Dr0nj/regente-server/internal/hub"
)

// fakePresence — RemotePresence mockado (agents em outros nós), p/ testar R5 sem NATS.
type fakePresence struct{ agents []bus.RemoteAgent }

func (f fakePresence) RemoteAgents() []bus.RemoteAgent { return f.agents }

func findAgent(fleet []agentRow, id string) *agentRow {
	for i := range fleet {
		if fleet[i].ID == id {
			return &fleet[i]
		}
	}
	return nil
}

// Frota consolidada: online (no hub) + offline (só no DB), com metadata e capabilities.
func TestAgents_Fleet(t *testing.T) {
	d := newTestDB(t)
	h := hub.New()
	cfg := Config{DB: d, Hub: h, Token: "test-token"}
	s := &server{cfg: cfg}
	srv := httptest.NewServer(NewRouter(cfg))
	defer srv.Close()
	defer d.Close()

	// Agente ONLINE: registrado no hub + gravado no DB.
	online := &hub.Client{
		ID: "agent-a", Kind: hub.ClientAgent, Capabilities: []string{"COMMAND", "WASM"},
		OS: "linux", Arch: "amd64", Host: "box1", Version: "0.1.0",
		Started: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	}
	h.Register(online)
	s.recordAgentConnect(online)

	// Agente OFFLINE: gravado no DB mas NÃO no hub.
	offline := &hub.Client{ID: "agent-b", Kind: hub.ClientAgent, Capabilities: []string{"COMMAND"}, OS: "windows", Arch: "amd64", Host: "box2"}
	s.recordAgentConnect(offline)

	var fleet []agentRow
	getJSON(t, srv, "/api/agents", &fleet)
	if len(fleet) != 2 {
		t.Fatalf("esperava 2 agentes na frota, veio %d", len(fleet))
	}

	a := findAgent(fleet, "agent-a")
	if a == nil || !a.Online {
		t.Fatalf("agent-a deveria estar online, veio %+v", a)
	}
	if a.OS != "linux" || a.Arch != "amd64" || a.Host != "box1" || a.Version != "0.1.0" {
		t.Fatalf("metadata de agent-a errada: %+v", a)
	}
	if len(a.Capabilities) != 2 || a.StartedAt == nil {
		t.Fatalf("agent-a: capabilities/startedAt errado: %+v", a)
	}

	b := findAgent(fleet, "agent-b")
	if b == nil || b.Online {
		t.Fatalf("agent-b deveria estar OFFLINE, veio %+v", b)
	}
	if b.OS != "windows" || b.StartedAt != nil {
		t.Fatalf("agent-b: metadata errada (startedAt deveria ser nil sem 'started'): %+v", b)
	}
	if a.LastSeen == nil || b.LastSeen == nil {
		t.Fatal("last_seen deveria estar setado nos dois")
	}
}

// R5 — presença cross-nó: um agent conectado em OUTRO nó aparece ONLINE (não
// fantasma offline), com node do nó dono e local=false (não é pingável daqui);
// um agent remoto SEM linha no DB deste nó ainda entra na frota do cluster.
func TestAgents_CrossNodePresence(t *testing.T) {
	d := newTestDB(t)
	h := hub.New()
	presence := fakePresence{agents: []bus.RemoteAgent{
		// agent-remote: metadata no DB (offline aqui), mas online no node-2.
		{ID: "agent-remote", Node: "node-2", Caps: []string{"COMMAND"}, LastSeen: time.Now()},
		// agent-ghost: só presença remota, sem linha no DB deste nó.
		{ID: "agent-ghost", Node: "node-3", Caps: []string{"WASM"}, LastSeen: time.Now()},
	}}
	cfg := Config{DB: d, Hub: h, Token: "test-token", Presence: presence, NodeID: "node-1"}
	s := &server{cfg: cfg}
	srv := httptest.NewServer(NewRouter(cfg))
	defer srv.Close()
	defer d.Close()

	// Local online (node-1) + o remoto que também tem linha no DB.
	local := &hub.Client{ID: "agent-local", Kind: hub.ClientAgent, Capabilities: []string{"COMMAND"}, OS: "linux"}
	h.Register(local)
	s.recordAgentConnect(local)
	s.recordAgentConnect(&hub.Client{ID: "agent-remote", Kind: hub.ClientAgent, Capabilities: []string{"COMMAND"}, OS: "linux"})

	var fleet []agentRow
	getJSON(t, srv, "/api/agents", &fleet)
	if len(fleet) != 3 {
		t.Fatalf("esperava 3 (local + remoto-no-DB + ghost), veio %d: %+v", len(fleet), fleet)
	}

	loc := findAgent(fleet, "agent-local")
	if loc == nil || !loc.Online || !loc.Local || loc.Node != "node-1" {
		t.Fatalf("agent-local deveria ser online+local no node-1, veio %+v", loc)
	}

	rem := findAgent(fleet, "agent-remote")
	if rem == nil || !rem.Online || rem.Local || rem.Node != "node-2" {
		t.Fatalf("agent-remote deveria ser online (node-2) e NÃO local, veio %+v", rem)
	}

	ghost := findAgent(fleet, "agent-ghost")
	if ghost == nil || !ghost.Online || ghost.Local || ghost.Node != "node-3" {
		t.Fatalf("agent-ghost (só presença) deveria aparecer online no node-3, veio %+v", ghost)
	}
}

// Upsert no reconnect: atualiza metadata mas PRESERVA o registro (first_seen).
func TestAgents_UpsertPreservesFirstSeen(t *testing.T) {
	d := newTestDB(t)
	h := hub.New()
	cfg := Config{DB: d, Hub: h, Token: "test-token"}
	s := &server{cfg: cfg}
	srv := httptest.NewServer(NewRouter(cfg))
	defer srv.Close()
	defer d.Close()

	s.recordAgentConnect(&hub.Client{ID: "ag", Capabilities: []string{"COMMAND"}, OS: "linux", Host: "old"})
	var f1 []agentRow
	getJSON(t, srv, "/api/agents", &f1)
	if len(f1) != 1 || f1[0].FirstSeen == nil {
		t.Fatalf("esperava 1 agente com first_seen, veio %+v", f1)
	}
	first := *f1[0].FirstSeen

	// reconnect com host/OS diferentes → atualiza metadata, NÃO duplica, mantém first_seen.
	s.recordAgentConnect(&hub.Client{ID: "ag", Capabilities: []string{"COMMAND", "HTTP"}, OS: "windows", Host: "new"})
	var f2 []agentRow
	getJSON(t, srv, "/api/agents", &f2)
	if len(f2) != 1 {
		t.Fatalf("reconnect NÃO deveria duplicar, veio %d linhas", len(f2))
	}
	if f2[0].OS != "windows" || f2[0].Host != "new" || len(f2[0].Capabilities) != 2 {
		t.Fatalf("metadata deveria ter atualizado, veio %+v", f2[0])
	}
	if f2[0].FirstSeen == nil || !f2[0].FirstSeen.Equal(first) {
		t.Fatalf("first_seen deveria ser preservado (%v), veio %v", first, f2[0].FirstSeen)
	}
}
