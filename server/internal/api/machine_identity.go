package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/go-chi/chi/v5"
)

const machineRevalidateInterval = time.Second

type machinePrincipal struct {
	CredentialID int64
	AgentID      string
	Environment  string
	Capabilities string
}

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Authorization evita segredos em URLs, histórico e access logs de proxies.
func (s *server) machineAuth(r *http.Request) (*machinePrincipal, bool) {
	header := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || len(token) > 256 || token == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	p := &machinePrincipal{}
	err := s.cfg.DB.QueryRowContext(ctx, `SELECT t.id, p.agent_id, p.environment, p.capabilities
 FROM agent_tokens t JOIN machine_principals p ON p.agent_id=t.agent_id
 WHERE t.token_hash=? AND t.revoked_at=0 AND t.expires_at>?`, tokenDigest(token), time.Now().UnixMilli()).Scan(
		&p.CredentialID, &p.AgentID, &p.Environment, &p.Capabilities)
	if err == nil {
		_, _ = s.cfg.DB.ExecContext(ctx, `UPDATE agent_tokens SET last_used_at=CURRENT_TIMESTAMP WHERE id=?`, p.CredentialID)
	}
	return p, err == nil
}

func (s *server) machineValid(p *machinePrincipal) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var n int
	err := s.cfg.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_tokens t
 JOIN machine_principals p ON p.agent_id=t.agent_id
 WHERE t.id=? AND t.revoked_at=0 AND t.expires_at>? AND p.agent_id=?
 AND p.environment=? AND p.capabilities=?`, p.CredentialID, time.Now().UnixMilli(),
		p.AgentID, p.Environment, p.Capabilities).Scan(&n)
	return err == nil && n == 1
}

func canonicalCaps(caps []string) string {
	out := append([]string(nil), caps...)
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	slices.Sort(out)
	return strings.Join(slices.Compact(out), ",")
}

// Claims de transporte precisam coincidir com o principal provisionado.
func (p *machinePrincipal) matches(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("id") == p.AgentID && q.Get("env") == p.Environment &&
		canonicalCaps(strings.Split(q.Get("caps"), ",")) == p.Capabilities
}

func (s *server) machineHandshake(w http.ResponseWriter, r *http.Request) (*machinePrincipal, bool) {
	p, ok := s.machineAuth(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	if !p.matches(r) {
		http.Error(w, "agent identity or scope mismatch", http.StatusForbidden)
		return nil, false
	}
	return p, true
}

func (s *server) secureMachineClient(c *hub.Client, p *machinePrincipal) {
	c.CredentialID = p.CredentialID
	c.StrictIdentity = true
	c.Authorize = func() bool { return s.machineValid(p) }
}

func (s *server) watchMachine(c *hub.Client) {
	go func() {
		ticker := time.NewTicker(machineRevalidateInterval)
		defer ticker.Stop()
		for {
			select {
			case <-c.Done:
				return
			case <-ticker.C:
				if !c.Authorize() {
					s.cfg.Hub.Unregister(c)
					return
				}
			}
		}
	}()
}

// Desbloqueia também um Flush/Write HTTP parado num peer que deixou de ler.
func closeMachineStream(w http.ResponseWriter, c *hub.Client) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-c.Done:
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now())
		case <-done:
		}
	}()
	return func() { close(done); <-stopped; _ = http.NewResponseController(w).SetWriteDeadline(time.Time{}) }
}

// Atribuição atual, não fencing de tentativa: executionId pertence a I08–I10.
func (s *server) machineOwns(p *machinePrincipal, instanceID string) bool {
	if instanceID == "" || !s.machineValid(p) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var n int
	err := s.cfg.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM instances
 WHERE id=? AND agent_id=? AND status='RUNNING'`, instanceID, p.AgentID).Scan(&n)
	return err == nil && n == 1
}

type tokenRequest struct {
	Label        string    `json:"label"`
	AgentID      string    `json:"agentId"`
	Environment  string    `json:"environment"`
	Capabilities []string  `json:"capabilities"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

var machineName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var machineCap = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)

func (b *tokenRequest) validate() bool {
	if !machineName.MatchString(b.AgentID) || strings.EqualFold(b.AgentID, "SERVER-AGENT") ||
		strings.HasPrefix(strings.ToLower(b.AgentID), "web-") || len(b.Label) > 256 {
		return false
	}
	if b.Environment != "" && !machineName.MatchString(b.Environment) {
		return false
	}
	if len(b.Capabilities) == 0 || len(b.Capabilities) > 32 {
		return false
	}
	for _, c := range b.Capabilities {
		if !machineCap.MatchString(c) {
			return false
		}
	}
	return validMachineExpiry(b.ExpiresAt)
}

func validMachineExpiry(t time.Time) bool {
	now := time.Now()
	return t.After(now) && !t.After(now.Add(365*24*time.Hour))
}

func issueMachineToken(tx *db.Tx, b tokenRequest) (map[string]any, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	token := "rgta_" + hex.EncodeToString(secret)
	var id int64
	err := tx.QueryRow(`INSERT INTO agent_tokens(token_hash, token_prefix, label, agent_id, expires_at)
 VALUES(?,?,?,?,?) RETURNING id`, tokenDigest(token), token[:13], b.Label, b.AgentID,
		b.ExpiresAt.UnixMilli()).Scan(&id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "label": b.Label, "agentId": b.AgentID, "environment": b.Environment,
		"capabilities": strings.Split(canonicalCaps(b.Capabilities), ","), "expiresAt": b.ExpiresAt.UTC(), "token": token}, nil
}

func (s *server) createAgentToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var b tokenRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b) != nil || !b.validate() {
		http.Error(w, "agentId, environment, capabilities and expiresAt (within 365 days) are required; reserved IDs are forbidden", http.StatusBadRequest)
		return
	}
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		http.Error(w, "credential storage unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO machine_principals(agent_id, environment, capabilities) VALUES(?,?,?)
 ON CONFLICT(agent_id) DO NOTHING`, b.AgentID, b.Environment, canonicalCaps(b.Capabilities))
	if err != nil {
		http.Error(w, "could not provision principal", http.StatusInternalServerError)
		return
	}
	var env, caps string
	if tx.QueryRow(`SELECT environment,capabilities FROM machine_principals WHERE agent_id=?`, b.AgentID).Scan(&env, &caps) != nil ||
		env != b.Environment || caps != canonicalCaps(b.Capabilities) {
		http.Error(w, "principal scope is immutable; use a new agent ID for a different scope", http.StatusConflict)
		return
	}
	result, err := issueMachineToken(tx, b)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		http.Error(w, "could not issue credential", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, result)
}

func (s *server) rotateAgentToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var b struct {
		ExpiresAt    time.Time `json:"expiresAt"`
		GraceSeconds int       `json:"graceSeconds"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b) != nil ||
		!validMachineExpiry(b.ExpiresAt) || b.GraceSeconds < 0 || b.GraceSeconds > 3600 {
		http.Error(w, "invalid expiry or graceSeconds (0..3600)", http.StatusBadRequest)
		return
	}
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		http.Error(w, "credential storage unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()
	// O UPDATE adquire lock e condiciona a rotação à credencial ainda ativa.
	now := time.Now().UnixMilli()
	until := now + int64(b.GraceSeconds)*1000
	res, err := tx.Exec(`UPDATE agent_tokens SET rotated_at=?, expires_at=CASE WHEN expires_at<? THEN expires_at ELSE ? END
 WHERE id=? AND agent_id IS NOT NULL AND revoked_at=0 AND rotated_at=0 AND expires_at>?`, now, until, until, chi.URLParam(r, "id"), now)
	if err != nil {
		http.Error(w, "could not rotate credential", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		http.Error(w, "active bound credential not found", http.StatusNotFound)
		return
	}
	var req tokenRequest
	var caps string
	err = tx.QueryRow(`SELECT t.label,p.agent_id,p.environment,p.capabilities FROM agent_tokens t
 JOIN machine_principals p ON p.agent_id=t.agent_id WHERE t.id=?`, chi.URLParam(r, "id")).Scan(&req.Label, &req.AgentID, &req.Environment, &caps)
	if err != nil {
		http.Error(w, "could not load principal", http.StatusInternalServerError)
		return
	}
	req.Capabilities = strings.Split(caps, ",")
	req.ExpiresAt = b.ExpiresAt
	result, err := issueMachineToken(tx, req)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		http.Error(w, "could not rotate credential", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, result)
}

func (s *server) listAgentTokens(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	rows, err := s.cfg.DB.Query(`SELECT t.id,t.label,t.token_prefix,t.created_at,t.last_used_at,
 COALESCE(p.agent_id,''),COALESCE(p.environment,''),COALESCE(p.capabilities,''),t.expires_at,t.revoked_at
 FROM agent_tokens t LEFT JOIN machine_principals p ON p.agent_id=t.agent_id ORDER BY t.id DESC`)
	if err != nil {
		http.Error(w, "credential storage unavailable", http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, expiry, revoked int64
		var label, prefix, agent, env, caps string
		var created, last any
		if err = rows.Scan(&id, &label, &prefix, &created, &last, &agent, &env, &caps, &expiry, &revoked); err != nil {
			http.Error(w, "could not read credentials", http.StatusInternalServerError)
			return
		}
		status := "active"
		if agent == "" {
			status = "requires_reissue"
		} else if revoked != 0 {
			status = "revoked"
		} else if expiry <= time.Now().UnixMilli() {
			status = "expired"
		}
		capList := []string{}
		if caps != "" {
			capList = strings.Split(caps, ",")
		}
		out = append(out, map[string]any{"id": id, "label": label, "tokenPrefix": prefix, "agentId": agent,
			"environment": env, "capabilities": capList, "expiresAt": time.UnixMilli(expiry).UTC(), "status": status,
			"createdAt": created, "lastUsedAt": last})
	}
	if !rowsOK(w, rows) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}

func (s *server) revokeAgentToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	res, err := s.cfg.DB.Exec(`UPDATE agent_tokens SET revoked_at=? WHERE id=?`, time.Now().UnixMilli(), chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "could not revoke credential", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		http.Error(w, "credential not found", http.StatusNotFound)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "revoked"})
}
