// F18 \u2014 Variables REST CRUD (global scope).
package api

import (
	"encoding/json"
	"net/http"

	"github.com/Dr0nj/regente-server/internal/auth"
)

// listVariables \u2014 GET /api/variables \u2192 array ordenado por nome.
func (s *server) listVariables(w http.ResponseWriter, r *http.Request) {
	vs := s.cfg.Scheduler.Variables()
	if vs == nil {
		writeJSON(w, 200, []any{})
		return
	}
	writeJSON(w, 200, vs.List())
}

// putVariable \u2014 PUT /api/variables/{name}  body: {"value": "..."} (admin).
func (s *server) putVariable(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	vs := s.cfg.Scheduler.Variables()
	if vs == nil {
		http.Error(w, "variables not configured", 503)
		return
	}
	name := urlName(r, "name")
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	actor := ""
	if u, ok := auth.FromContext(r.Context()); ok && u != nil {
		actor = u.Username
	}
	v, err := vs.Set(name, body.Value, actor)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if s.cfg.Hub != nil {
		s.cfg.Hub.BroadcastWeb("variables.changed", map[string]any{"name": name, "action": "set"})
	}
	writeJSON(w, 200, v)
}

// deleteVariable \u2014 DELETE /api/variables/{name} (admin, idempotente).
func (s *server) deleteVariable(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	vs := s.cfg.Scheduler.Variables()
	if vs == nil {
		http.Error(w, "variables not configured", 503)
		return
	}
	name := urlName(r, "name")
	if err := vs.Delete(name); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if s.cfg.Hub != nil {
		s.cfg.Hub.BroadcastWeb("variables.changed", map[string]any{"name": name, "action": "delete"})
	}
	w.WriteHeader(204)
}
