// Package bus — R5: hub distribuído sobre um transporte pub/sub (NATS), mantendo
// o hub WebSocket por-processo como camada local. Resolve o "agente preso a um nó":
//
//   - fan-out de eventos web entre nós (BroadcastWeb chega aos web clients de TODOS);
//   - presença de agents por nó (cada nó publica seus agents; os outros mantêm um
//     mapa dos agents remotos, com TTL);
//   - roteamento de dispatch: se o agent não é local, publica o payload no subject
//     do nó dono, que entrega ao seu agent local.
//
// Segurança de execução única: só o LÍDER despacha (o tick é leader-gated), então a
// escolha de qual agent recebe o job é feita por um decisor único — sem corrida nem
// dupla-execução entre nós. O transporte é abstraído por Transport para ser testável
// sem um NATS real. Defaults do server (-bus=hub) usam só o hub local — comportamento
// idêntico ao anterior; o modo distribuído é opt-in via -bus=nats.
package bus

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
)

const (
	subjWeb            = "regente.web"       // eventos web (fan-out)
	subjPresence       = "regente.presence"  // presença de agents por nó
	subjDispatchPrefix = "regente.dispatch." // + nodeID — dispatch roteado ao nó dono
)

// Transport — pub/sub mínimo. NATS em produção (nats.go), fake nos testes.
type Transport interface {
	Publish(subject string, data []byte) error
	Subscribe(subject string, handler func(data []byte)) error
}

type remoteAgent struct {
	node     string
	caps     []string
	env      string // ADV-2 — ambiente do agente (roteamento por env cross-nó)
	lastSeen time.Time
}

// Distributed implementa scheduler.Bus por cima do hub local + um Transport.
type Distributed struct {
	node  string
	local *hub.Hub
	tr    Transport

	mu     sync.RWMutex
	remote map[string]remoteAgent // agentID -> nó dono + caps (presença remota)

	ttl      time.Duration // expira presença remota não renovada
	interval time.Duration // cadência de publicação de presença
}

// NewDistributed cria o bus distribuído. `node` deve ser único por processo no cluster.
func NewDistributed(node string, local *hub.Hub, tr Transport) *Distributed {
	return &Distributed{
		node:     node,
		local:    local,
		tr:       tr,
		remote:   map[string]remoteAgent{},
		ttl:      15 * time.Second,
		interval: 5 * time.Second,
	}
}

type webEnvelope struct {
	Node    string      `json:"node"`
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
}
type agentInfo struct {
	ID   string   `json:"id"`
	Caps []string `json:"caps"`
	Env  string   `json:"env,omitempty"` // ADV-2
}
type presenceMsg struct {
	Node   string      `json:"node"`
	Agents []agentInfo `json:"agents"`
}
type routedDispatch struct {
	TargetAgent string          `json:"agent"`
	Raw         json.RawMessage `json:"raw"`
}

// Start assina os subjects e começa a publicar presença periodicamente.
func (d *Distributed) Start() error {
	if err := d.tr.Subscribe(subjWeb, d.onWeb); err != nil {
		return err
	}
	if err := d.tr.Subscribe(subjPresence, d.onPresence); err != nil {
		return err
	}
	if err := d.tr.Subscribe(subjDispatchPrefix+d.node, d.onRoutedDispatch); err != nil {
		return err
	}
	go d.presenceLoop()
	return nil
}

func (d *Distributed) presenceLoop() {
	t := time.NewTicker(d.interval)
	defer t.Stop()
	d.publishPresence()
	for range t.C {
		d.publishPresence()
	}
}

// ── scheduler.Bus ──

func (d *Distributed) BroadcastWeb(event string, payload interface{}) {
	d.local.BroadcastWeb(event, payload) // web clients deste nó
	data, _ := json.Marshal(webEnvelope{Node: d.node, Event: event, Payload: payload})
	if err := d.tr.Publish(subjWeb, data); err != nil {
		log.Printf("[bus] publish web: %v", err)
	}
}

func (d *Distributed) PickAgent(capability, env string) *hub.Client {
	return d.local.PickAgent(capability, env)
}
func (d *Distributed) GetAgent(id string) *hub.Client { return d.local.GetAgent(id) }

// HasAgent — disponibilidade local OU remota (presença R5 com TTL). O guard do
// scheduler usa isto; checar só o hub local starvaria dispatch roteado via NATS.
func (d *Distributed) HasAgent(agentID, capability, env string) bool {
	if d.local.HasAgent(agentID, capability, env) {
		return true
	}
	node, _ := d.findRemote(agentID, capability, env)
	return node != ""
}

func (d *Distributed) Dispatch(agentID, capability, env string, raw []byte) (hub.DispatchOutcome, string) {
	// 1. Agent local? mantém a semântica exata do hub (Sent/QueueFull).
	if out, id := d.local.Dispatch(agentID, capability, env, raw); out != hub.DispatchNoAgent {
		return out, id
	}
	// 2. Agent remoto (via presença): roteia o payload ao nó dono.
	node, id := d.findRemote(agentID, capability, env)
	if node == "" {
		return hub.DispatchNoAgent, ""
	}
	data, _ := json.Marshal(routedDispatch{TargetAgent: id, Raw: json.RawMessage(raw)})
	if err := d.tr.Publish(subjDispatchPrefix+node, data); err != nil {
		log.Printf("[bus] route dispatch -> %s: %v", node, err)
		return hub.DispatchNoAgent, ""
	}
	return hub.DispatchSent, id
}

// RemoteAgent — presença de um agent conectado em OUTRO nó (snapshot p/ a UI de
// Agentes). É o que faltava pra frota refletir a frota do CLUSTER, não só deste nó.
type RemoteAgent struct {
	ID       string
	Node     string
	Caps     []string
	Env      string // ADV-2 — ambiente do agente
	LastSeen time.Time
}

// RemoteAgents devolve os agents vistos em OUTROS nós dentro do TTL de presença.
// Alimenta GET /api/agents pra mostrar quem está online no cluster inteiro (R5).
func (d *Distributed) RemoteAgents() []RemoteAgent {
	d.mu.RLock()
	defer d.mu.RUnlock()
	now := time.Now()
	out := make([]RemoteAgent, 0, len(d.remote))
	for id, ra := range d.remote {
		if now.Sub(ra.lastSeen) > d.ttl {
			continue // presença expirada (o nó dono parou de publicar)
		}
		out = append(out, RemoteAgent{
			ID:       id,
			Node:     ra.node,
			Caps:     append([]string(nil), ra.caps...),
			Env:      ra.env,
			LastSeen: ra.lastSeen,
		})
	}
	return out
}

func (d *Distributed) findRemote(agentID, capability, env string) (node, id string) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if agentID != "" {
		if ra, ok := d.remote[agentID]; ok && hub.EnvMatch(env, ra.env) {
			return ra.node, agentID
		}
		return "", ""
	}
	for aid, ra := range d.remote {
		if !hub.EnvMatch(env, ra.env) {
			continue
		}
		for _, c := range ra.caps {
			if c == capability {
				return ra.node, aid
			}
		}
	}
	return "", ""
}

// ── handlers ──

func (d *Distributed) onWeb(data []byte) {
	var env webEnvelope
	if json.Unmarshal(data, &env) != nil || env.Node == d.node {
		return // ignora o que nós mesmos publicamos
	}
	d.local.BroadcastWeb(env.Event, env.Payload)
}

func (d *Distributed) onPresence(data []byte) {
	var m presenceMsg
	if json.Unmarshal(data, &m) != nil || m.Node == d.node {
		return
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, a := range m.Agents {
		d.remote[a.ID] = remoteAgent{node: m.Node, caps: a.Caps, env: a.Env, lastSeen: now}
	}
	// Expira presença remota não renovada (agent desconectou do nó dono).
	for id, ra := range d.remote {
		if now.Sub(ra.lastSeen) > d.ttl {
			delete(d.remote, id)
		}
	}
}

func (d *Distributed) onRoutedDispatch(data []byte) {
	var env routedDispatch
	if json.Unmarshal(data, &env) != nil {
		return
	}
	a := d.local.GetAgent(env.TargetAgent)
	if a == nil {
		return // agent saiu deste nó; o watchdog de stuck-running do scheduler cobre
	}
	select {
	case a.Send <- []byte(env.Raw):
	default:
		log.Printf("[bus] dispatch roteado p/ %s: buffer cheio", env.TargetAgent)
	}
}

func (d *Distributed) publishPresence() {
	online := d.local.OnlineAgents() // []map{id, capabilities, environment, …}
	agents := make([]agentInfo, 0, len(online))
	for _, a := range online {
		id, _ := a["id"].(string)
		caps, _ := a["capabilities"].([]string)
		env, _ := a["environment"].(string)
		agents = append(agents, agentInfo{ID: id, Caps: caps, Env: env})
	}
	data, _ := json.Marshal(presenceMsg{Node: d.node, Agents: agents})
	if err := d.tr.Publish(subjPresence, data); err != nil {
		log.Printf("[bus] publish presence: %v", err)
	}
}
