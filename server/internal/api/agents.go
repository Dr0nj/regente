// B5 — tokens por agente. Cada agente conecta no /ws/agent com o SEU token
// (criado/revogado pelo admin), em vez do dev-token compartilhado.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

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
