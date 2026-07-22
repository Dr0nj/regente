package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // dev: permissivo; em prod: checar origin
}

func randID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *server) wsWeb(w http.ResponseWriter, r *http.Request) {
	if !s.wsTokenOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws/web] upgrade: %v", err)
		return
	}
	c := &hub.Client{
		ID:   "web-" + randID(),
		Kind: hub.ClientWeb,
		Conn: conn,
		Send: make(chan []byte, 64),
	}
	s.cfg.Hub.Register(c)
	go clientWriter(c)
	clientReader(c, nil)
	s.cfg.Hub.Unregister(c)
}

func (s *server) wsAgent(w http.ResponseWriter, r *http.Request) {
	// B5 — aceita token POR AGENTE (agent_tokens) ou o token legado/sessão (dev).
	if !s.agentTokenValid(auth.ExtractToken(r)) && !s.wsTokenOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	agentID := r.URL.Query().Get("id")
	if agentID == "" {
		agentID = "agent-" + randID()
	}
	capStr := r.URL.Query().Get("caps")
	var caps []string
	if capStr != "" {
		caps = strings.Split(capStr, ",")
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws/agent] upgrade: %v", err)
		return
	}
	q := r.URL.Query()
	c := &hub.Client{
		ID:           agentID,
		Kind:         hub.ClientAgent,
		Conn:         conn,
		Send:         make(chan []byte, 64),
		Capabilities: caps,
		Environment:  q.Get("env"), // ADV-2 — label de ambiente/site (flag -env do agente)
		OS:           q.Get("os"),
		Arch:         q.Get("arch"),
		Host:         q.Get("host"),
		Version:      q.Get("ver"),
		Started:      q.Get("started"),
	}
	s.cfg.Hub.Register(c)
	s.recordAgentConnect(c)
	log.Printf("[ws/agent] %s connected caps=%v env=%q os=%s/%s host=%s", agentID, caps, c.Environment, c.OS, c.Arch, c.Host)
	// Agente voltou: (1) avisa a UI (cards WAIT AGENT re-derivam na hora) e
	// (2) cutuca um Tick — jobs parados esperando agente disparam IMEDIATAMENTE,
	// em vez de aguardar o próximo ciclo. Tick é idempotente e leader-gated.
	s.cfg.Hub.BroadcastWeb("agent.changed", map[string]any{"id": agentID, "state": "connected", "caps": caps, "environment": c.Environment})
	if s.cfg.Scheduler != nil {
		go s.cfg.Scheduler.Tick()
	}

	go clientWriter(c)
	clientReader(c, func(msg []byte) {
		var ev struct {
			Event      string `json:"event"`
			InstanceID string `json:"instanceId"`
			ExitCode   int    `json:"exitCode"`
			Output     string `json:"output"`
			Chunk      string `json:"chunk"`
			PingID     string `json:"pingId"`
		}
		if err := json.Unmarshal(msg, &ev); err != nil {
			return
		}
		switch ev.Event {
		case "pong":
			// Ping ativo: destrava o handler HTTP que espera o round-trip.
			s.cfg.Hub.Touch(agentID)
			if s.pings != nil {
				s.pings.signal(ev.PingID)
			}
		case "result":
			status := domain.StatusOK
			if ev.ExitCode != 0 {
				status = domain.StatusNotOK
			}
			s.cfg.Scheduler.FinishInstance(ev.InstanceID, status, ev.ExitCode, ev.Output)
		case "output":
			// OL-1 — stream de stdout/stderr: APPENDa em instance_output (por
			// tentativa, live-tail da aba Output), NÃO em instance_events. Assim o
			// sysout sai da trilha de auditoria/feed. Chunk gravado verbatim (com
			// as quebras de linha) — o concat reproduz o stream fielmente.
			if ev.InstanceID != "" && ev.Chunk != "" {
				s.cfg.Scheduler.AppendOutput(ev.InstanceID, ev.Chunk)
			}
		case "heartbeat":
			s.cfg.Hub.Touch(agentID)
			s.recordAgentSeen(agentID)
		}
	})
	s.cfg.Hub.Unregister(c)
	s.recordAgentSeen(agentID) // marca o último visto na desconexão
	log.Printf("[ws/agent] %s disconnected", agentID)
	// Avisa a UI: jobs desse agente (ou da capability que só ele tinha) devem
	// re-derivar pra WAIT AGENT.
	s.cfg.Hub.BroadcastWeb("agent.changed", map[string]any{"id": agentID, "state": "disconnected"})
}

func clientWriter(c *hub.Client) {
	for msg := range c.Send {
		if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
	_ = c.Conn.Close()
}

func clientReader(c *hub.Client, onMsg func([]byte)) {
	defer c.Conn.Close()
	for {
		_, msg, err := c.Conn.ReadMessage()
		if err != nil {
			return
		}
		if onMsg != nil {
			onMsg(msg)
		}
	}
}
