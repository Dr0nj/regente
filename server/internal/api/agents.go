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
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/go-chi/chi/v5"
)

// agentRow — linha da tela de Agentes: metadata persistida + online (verdade do hub).
type agentRow struct {
	ID           string     `json:"id"`
	OS           string     `json:"os,omitempty"`
	Arch         string     `json:"arch,omitempty"`
	Host         string     `json:"host,omitempty"`
	Version      string     `json:"version,omitempty"`
	Capabilities []string   `json:"capabilities"`
	Online       bool       `json:"online"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`   // início do processo (uptime)
	ConnectedAt  *time.Time `json:"connectedAt,omitempty"` // conectou neste servidor (sessão)
	FirstSeen    *time.Time `json:"firstSeen,omitempty"`
	LastSeen     *time.Time `json:"lastSeen,omitempty"`
}

// listAgents — GET /api/agents: frota CONSOLIDADA (online + offline com last-seen).
// Online = verdade do hub deste nó; metadata/last-seen vêm da tabela agents (v6).
func (s *server) listAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.cfg.DB.Query(
		`SELECT id, os, arch, host, version, capabilities, started_at, connected_at, first_seen, last_seen_at
		 FROM agents ORDER BY id`,
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
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
		a.Online = s.cfg.Hub != nil && s.cfg.Hub.IsOnline(a.ID)
		a.StartedAt = nullTimePtr(started)
		a.ConnectedAt = nullTimePtr(connected)
		a.FirstSeen = nullTimePtr(first)
		a.LastSeen = nullTimePtr(last)
		out = append(out, a)
	}
	writeJSON(w, 200, out)
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
