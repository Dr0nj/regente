package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

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
	if !s.allowedOrigin(r) {
		http.Error(w, "Origin is not allowed", http.StatusForbidden)
		return
	}
	var digest string
	ticket := r.URL.Query().Get("ticket")
	err := s.cfg.DB.QueryRow("DELETE FROM web_tickets WHERE token_hash=? AND expires_at>? RETURNING session_token", auth.Digest(ticket), time.Now()).Scan(&digest)
	access, accessErr := s.webAccess(digest)
	if err != nil || ticket == "" || accessErr != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	fingerprint := access.fingerprint()
	valid := func() bool {
		current, e := s.webAccess(digest)
		return e == nil && current.fingerprint() == fingerprint
	}
	var environment *string
	if values, ok := r.URL.Query()["environment"]; ok {
		if len(values) != 1 || len(values[0]) > 128 {
			http.Error(w, "Invalid event environment", http.StatusBadRequest)
			return
		}
		environment = &values[0]
	}
	view, _ := s.workspaceView(access, environment)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws/web] upgrade: %v", err)
		return
	}
	c := &hub.Client{
		ID:        "web-" + randID(),
		Kind:      hub.ClientWeb,
		Conn:      conn,
		Send:      make(chan []byte, 64),
		Authorize: valid,
	}
	c.FilterWeb = func(raw []byte) []byte {
		current, e := s.webAccess(digest)
		if e != nil || current.fingerprint() != fingerprint {
			_ = conn.Close()
			return nil
		}
		return s.filterWebEvent(current, environment, &view, raw)
	}
	conn.SetReadLimit(4096)
	s.cfg.Hub.Register(c)
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !valid() {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	go clientWriter(c)
	clientReader(c, nil)
	s.cfg.Hub.Unregister(c)
}

func (s *server) wsAgent(w http.ResponseWriter, r *http.Request) {
	// Mesmo gate exclusivo de máquina usado por HTTP e SSE.
	p, ok := s.machineHandshake(w, r)
	if !ok {
		return
	}
	agentID := p.AgentID
	caps := strings.Split(p.Capabilities, ",")
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
	s.secureMachineClient(c, p)
	s.cfg.Hub.Register(c)
	s.watchMachine(c)
	s.recordAgentConnect(c)
	log.Printf("[ws/agent] %s connected caps=%v env=%q os=%s/%s host=%s", agentID, caps, c.Environment, c.OS, c.Arch, c.Host)
	// Agente voltou: (1) avisa a UI (cards WAIT AGENT re-derivam na hora) e
	// (2) cutuca um Tick — jobs parados esperando agente disparam IMEDIATAMENTE,
	// em vez de aguardar o próximo ciclo. Tick é idempotente e leader-gated.
	s.broadcastWeb("agent.changed", map[string]any{"id": agentID, "state": "connected", "caps": caps, "environment": c.Environment})
	if s.cfg.Scheduler != nil {
		go s.cfg.Scheduler.Tick()
	}

	go clientWriter(c)
	clientReader(c, func(msg []byte) {
		if !s.machineValid(p) {
			s.cfg.Hub.Unregister(c)
			return
		}
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
			if !s.machineOwns(p, ev.InstanceID) {
				s.cfg.Hub.Unregister(c)
				return
			}
			status := domain.StatusOK
			if ev.ExitCode != 0 {
				status = domain.StatusNotOK
			}
			s.cfg.Scheduler.FinishInstance(ev.InstanceID, status, ev.ExitCode, ev.Output)
		case "output":
			if !s.machineOwns(p, ev.InstanceID) {
				s.cfg.Hub.Unregister(c)
				return
			}
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
	s.broadcastWeb("agent.changed", map[string]any{"id": agentID, "state": "disconnected"})
}

func clientWriter(c *hub.Client) {
	for msg := range c.Send {
		if c.Authorize != nil && !c.Authorize() {
			break
		}
		if c.Kind == hub.ClientWeb {
			if c.FilterWeb == nil {
				break
			}
			msg = c.FilterWeb(msg)
			if len(msg) == 0 {
				continue
			}
			_ = c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		}
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
