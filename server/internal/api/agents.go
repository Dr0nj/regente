// B5 — tokens por agente. Cada agente conecta no /ws/agent com o SEU token
// (criado/revogado pelo admin), em vez do dev-token compartilhado.
package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/bus"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/go-chi/chi/v5"
)

// pingRegistry — correlaciona o pong de um agente (chega pelo /ws/agent reader) com
// o handler HTTP que disparou o ping e espera a resposta. Round-trip = latência.
type pingRegistry struct {
	mu      sync.Mutex
	waiters map[string]chan struct{}
}

func newPingRegistry() *pingRegistry { return &pingRegistry{waiters: map[string]chan struct{}{}} }

func (p *pingRegistry) register(id string) <-chan struct{} {
	ch := make(chan struct{}, 1)
	p.mu.Lock()
	p.waiters[id] = ch
	p.mu.Unlock()
	return ch
}

func (p *pingRegistry) signal(id string) {
	p.mu.Lock()
	ch := p.waiters[id]
	p.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (p *pingRegistry) unregister(id string) {
	p.mu.Lock()
	delete(p.waiters, id)
	p.mu.Unlock()
}

type pingResult struct {
	ID        string `json:"id"`
	Online    bool   `json:"online"`
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	Error     string `json:"error,omitempty"`
}

// pingAgent — POST /api/agents/{id}/ping: round-trip ATIVO (ping/pong) pelo /ws/agent
// pra confirmar liveness + medir latência, em vez de só inferir presença pela conexão.
func (s *server) pingAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.cfg.Hub == nil || !s.cfg.Hub.IsOnline(id) {
		writeJSON(w, 200, pingResult{ID: id, Online: false, Error: "agente offline"})
		return
	}
	pingID := randID()
	ch := s.pings.register(pingID)
	defer s.pings.unregister(pingID)

	raw, _ := json.Marshal(map[string]string{"event": "ping", "pingId": pingID})
	sent := time.Now()
	if outcome, _ := s.cfg.Hub.Dispatch(id, "", "", raw); outcome != hub.DispatchSent {
		writeJSON(w, 200, pingResult{ID: id, Online: true, Error: "não foi possível enviar (buffer cheio?)"})
		return
	}
	select {
	case <-ch:
		writeJSON(w, 200, pingResult{ID: id, Online: true, OK: true, LatencyMs: time.Since(sent).Milliseconds()})
	case <-time.After(5 * time.Second):
		writeJSON(w, 200, pingResult{ID: id, Online: true, Error: "timeout (5s)"})
	}
}

// agentRow — linha da tela de Agentes: metadata persistida + online (verdade do hub).
type agentRow struct {
	ID           string     `json:"id"`
	OS           string     `json:"os,omitempty"`
	Arch         string     `json:"arch,omitempty"`
	Host         string     `json:"host,omitempty"`
	Version      string     `json:"version,omitempty"`
	Capabilities []string   `json:"capabilities"`
	Environment  string     `json:"environment,omitempty"` // ADV-2 — label runtime (flag -env); só quando online
	Online       bool       `json:"online"`
	Node         string     `json:"node,omitempty"`  // R5 — em qual nó do cluster está conectado
	Local        bool       `json:"local,omitempty"` // conectado NESTE nó (pingável via ws local)
	StartedAt    *time.Time `json:"startedAt,omitempty"`   // início do processo (uptime)
	ConnectedAt  *time.Time `json:"connectedAt,omitempty"` // conectou neste servidor (sessão)
	FirstSeen    *time.Time `json:"firstSeen,omitempty"`
	LastSeen     *time.Time `json:"lastSeen,omitempty"`
}

// listAgents — GET /api/agents: frota CONSOLIDADA (online + offline com last-seen).
// Online = verdade do hub deste nó UNIÃO a presença cross-nó (R5): um agent
// conectado em OUTRO nó do cluster aparece online (node = nó dono), não fantasma
// offline. Só o local (this node) é pingável — `local` diz isso pra UI.
// Metadata/last-seen vêm da tabela agents (v6, compartilhada entre nós no PG).
func (s *server) listAgents(w http.ResponseWriter, r *http.Request) {
	// Presença remota (bus distribuído): agentID -> nó dono. Vazio em single-node.
	remote := map[string]bus.RemoteAgent{}
	if s.cfg.Presence != nil {
		for _, ra := range s.cfg.Presence.RemoteAgents() {
			remote[ra.ID] = ra
		}
	}

	rows, err := s.cfg.DB.Query(
		`SELECT id, os, arch, host, version, capabilities, started_at, connected_at, first_seen, last_seen_at
		 FROM agents ORDER BY id`,
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	seen := map[string]bool{}
	out := []agentRow{}
	for rows.Next() {
		var a agentRow
		var caps string
		var started, connected, first, last sql.NullTime
		if err := rows.Scan(&a.ID, &a.OS, &a.Arch, &a.Host, &a.Version, &caps, &started, &connected, &first, &last); err != nil {
			continue
		}
		if caps != "" {
			a.Capabilities = strings.Split(caps, ",")
		} else {
			a.Capabilities = []string{}
		}
		a.StartedAt = nullTimePtr(started)
		a.ConnectedAt = nullTimePtr(connected)
		a.FirstSeen = nullTimePtr(first)
		a.LastSeen = nullTimePtr(last)
		s.applyPresence(&a, remote)
		seen[a.ID] = true
		out = append(out, a)
	}
	if !rowsOK(w, rows) {
		return
	}
	// Agents online em outro nó SEM linha no DB deste nó (nós com DBs separados):
	// ainda assim aparecem na frota do cluster.
	for id, ra := range remote {
		if seen[id] {
			continue
		}
		a := agentRow{ID: id, Capabilities: append([]string(nil), ra.Caps...)}
		if a.Capabilities == nil {
			a.Capabilities = []string{}
		}
		last := ra.LastSeen
		a.LastSeen = &last
		s.applyPresence(&a, remote)
		out = append(out, a)
	}
	writeJSON(w, 200, out)
}

// applyPresence resolve online/node/local de um agent: preferência ao hub LOCAL
// (conexão viva neste nó, pingável), senão à presença remota (R5, outro nó).
func (s *server) applyPresence(a *agentRow, remote map[string]bus.RemoteAgent) {
	if s.cfg.Hub != nil && s.cfg.Hub.IsOnline(a.ID) {
		a.Online = true
		a.Local = true
		a.Node = s.cfg.NodeID
		// ADV-2 — env é label RUNTIME (flag -env do processo): lê da conexão viva.
		if c := s.cfg.Hub.GetAgent(a.ID); c != nil {
			a.Environment = c.Environment
		}
		return
	}
	if ra, ok := remote[a.ID]; ok {
		a.Online = true
		a.Node = ra.Node
		a.Environment = ra.Env
		// A presença remota é mais fresca que o last_seen do DB deste nó.
		last := ra.LastSeen
		a.LastSeen = &last
	}
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	tt := t.Time
	return &tt
}

// recordAgentConnect — upsert da metadata do agente no connect (preserva first_seen:
// UPDATE; se 0 linhas, INSERT). Portável SQLite/PG sem depender de ON CONFLICT.
func (s *server) recordAgentConnect(c *hub.Client) {
	caps := strings.Join(c.Capabilities, ",")
	var started sql.NullTime
	if t, err := time.Parse(time.RFC3339, c.Started); err == nil {
		started = sql.NullTime{Time: t, Valid: true}
	}
	res, err := s.cfg.DB.Exec(
		`UPDATE agents SET os=?, arch=?, host=?, version=?, capabilities=?, started_at=?,
		        connected_at=CURRENT_TIMESTAMP, last_seen_at=CURRENT_TIMESTAMP WHERE id=?`,
		c.OS, c.Arch, c.Host, c.Version, caps, started, c.ID,
	)
	if err != nil {
		log.Printf("[agents] record connect %s: %v", c.ID, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// first_seen e last_seen_at (NOT NULL legado) preenchidos aqui; online (legado) = 0.
		if _, err := s.cfg.DB.Exec(
			`INSERT INTO agents(id, os, arch, host, version, capabilities, started_at, connected_at, first_seen, last_seen_at, online)
			 VALUES(?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,0)`,
			c.ID, c.OS, c.Arch, c.Host, c.Version, caps, started,
		); err != nil {
			log.Printf("[agents] insert %s: %v", c.ID, err)
		}
	}
}

// recordAgentSeen — carimba last_seen_at (heartbeat / desconexão).
func (s *server) recordAgentSeen(id string) {
	_, _ = s.cfg.DB.Exec(`UPDATE agents SET last_seen_at=CURRENT_TIMESTAMP WHERE id=?`, id)
}

// agentTokenValid valida um token de agente contra a tabela agent_tokens e,
// se válido, atualiza last_used_at. Usado no handshake do /ws/agent.
func (s *server) agentTokenValid(tok string) bool {
	if tok == "" {
		return false
	}
	var id int64
	if err := s.cfg.DB.QueryRow(`SELECT id FROM agent_tokens WHERE token=?`, tok).Scan(&id); err != nil {
		return false
	}
	_, _ = s.cfg.DB.Exec(`UPDATE agent_tokens SET last_used_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return true
}

// listAgentTokens — GET /api/agents/tokens (admin). Nunca devolve o token cru;
// só um prefixo pra identificação visual.
func (s *server) listAgentTokens(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	rows, err := s.cfg.DB.Query(`SELECT id, label, substr(token,1,8), created_at, COALESCE(last_used_at,'') FROM agent_tokens ORDER BY id DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var label, prefix, createdAt, lastUsed string
		if err := rows.Scan(&id, &label, &prefix, &createdAt, &lastUsed); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "label": label, "tokenPrefix": prefix + "…",
			"createdAt": createdAt, "lastUsedAt": lastUsed,
		})
	}
	if !rowsOK(w, rows) {
		return
	}
	writeJSON(w, 200, out)
}

// createAgentToken — POST /api/agents/tokens {label} (admin). Retorna o token
// cru UMA vez (não é recuperável depois).
func (s *server) createAgentToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		writeJSON(w, 500, map[string]string{"error": "rand: " + err.Error()})
		return
	}
	token := "rgta_" + hex.EncodeToString(b)
	id, err := s.cfg.DB.InsertID(`INSERT INTO agent_tokens(token, label) VALUES(?,?)`, token, body.Label)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "label": body.Label, "token": token})
}

// revokeAgentToken — DELETE /api/agents/tokens/{id} (admin).
func (s *server) revokeAgentToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := s.cfg.DB.Exec(`DELETE FROM agent_tokens WHERE id=?`, id); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "revoked"})
}
