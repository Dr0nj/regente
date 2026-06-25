// Package hub — WebSocket hub para clientes web (eventos live) e agents (dispatch).
//
// Web clients recebem broadcasts ("instance.changed", "definition.changed", ...).
// Agents recebem mensagens direcionadas com payload de dispatch e publicam "result".
package hub

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ClientKind string

const (
	ClientWeb   ClientKind = "web"
	ClientAgent ClientKind = "agent"
)

// Client — conexão WS ativa. Send buffer evita travar o writer.
type Client struct {
	ID           string
	Kind         ClientKind
	Conn         *websocket.Conn
	Send         chan []byte
	Capabilities []string // agents: ["COMMAND","REST",...]

	// Metadata de agente (reportada no handshake) — alimenta a tela de Agentes.
	OS          string
	Arch        string
	Host        string
	Version     string
	Started     string    // início do processo do agente (RFC3339), p/ uptime
	ConnectedAt time.Time // quando conectou neste servidor (sessão)
	LastSeen    time.Time // último sinal (heartbeat/result/output)
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
	agents  map[string]*Client // indexado por ID
}

func New() *Hub {
	return &Hub{
		clients: map[string]*Client{},
		agents:  map[string]*Client{},
	}
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if c.ConnectedAt.IsZero() {
		c.ConnectedAt = now
	}
	c.LastSeen = now
	h.clients[c.ID] = c
	if c.Kind == ClientAgent {
		h.agents[c.ID] = c
	}
}

// Touch carimba o último sinal de um agent (heartbeat/result/output) p/ a tela de
// Agentes. No-op se o agent não está conectado.
func (h *Hub) Touch(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if a := h.agents[id]; a != nil {
		a.LastSeen = time.Now()
	}
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c.ID]; !ok {
		return
	}
	delete(h.clients, c.ID)
	if c.Kind == ClientAgent {
		delete(h.agents, c.ID)
	}
	close(c.Send)
}

// BroadcastWeb envia um evento para todos os clientes web conectados.
func (h *Hub) BroadcastWeb(event string, payload interface{}) {
	msg := map[string]interface{}{"event": event, "payload": payload}
	raw, _ := json.Marshal(msg)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		if c.Kind != ClientWeb {
			continue
		}
		select {
		case c.Send <- raw:
		default:
			log.Printf("[hub] drop msg to web %s (buffer full)", c.ID)
		}
	}
}

// PickAgent retorna o primeiro agent disponível que anuncia a capability.
// V1: round-robin simples não implementado — pega o primeiro encontrado.
func (h *Hub) PickAgent(capability string) *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, a := range h.agents {
		for _, cap := range a.Capabilities {
			if cap == capability {
				return a
			}
		}
	}
	return nil
}

// GetAgent retorna o agent com ID exato (ou nil).
func (h *Hub) GetAgent(id string) *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.agents[id]
}

// DispatchOutcome — resultado de uma tentativa de entrega de dispatch a um agent.
type DispatchOutcome int

const (
	DispatchNoAgent   DispatchOutcome = iota // nenhum agent (local/remoto) com a capability/id
	DispatchSent                             // entregue ao agent (local) ou roteado ao nó dono
	DispatchQueueFull                        // agent local existe mas o buffer de Send está cheio
)

// Dispatch entrega o payload a um agent LOCAL, centralizando a lógica que antes
// vivia inline no scheduler (GetAgent por id, senão PickAgent por capability, e
// envio não-bloqueante no canal Send). É o seam que o bus distribuído (R5) estende
// para rotear ao nó dono quando o agent não está neste processo.
func (h *Hub) Dispatch(agentID, capability string, raw []byte) (DispatchOutcome, string) {
	var a *Client
	if agentID != "" {
		a = h.GetAgent(agentID)
	}
	if a == nil {
		a = h.PickAgent(capability)
	}
	if a == nil {
		return DispatchNoAgent, ""
	}
	select {
	case a.Send <- raw:
		return DispatchSent, a.ID
	default:
		return DispatchQueueFull, a.ID
	}
}

// OnlineAgents retorna os agents conectados com sua metadata (p/ a tela de Agentes).
func (h *Hub) OnlineAgents() []map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := []map[string]interface{}{}
	for id, a := range h.agents {
		out = append(out, map[string]interface{}{
			"id":           id,
			"capabilities": a.Capabilities,
			"os":           a.OS,
			"arch":         a.Arch,
			"host":         a.Host,
			"version":      a.Version,
			"started":      a.Started,
			"connectedAt":  a.ConnectedAt,
			"lastSeen":     a.LastSeen,
		})
	}
	return out
}

// IsOnline reporta se o agent está conectado AGORA neste nó (verdade do hub).
func (h *Hub) IsOnline(id string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.agents[id] != nil
}
