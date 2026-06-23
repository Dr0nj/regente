package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Dr0nj/regente-server/internal/audit"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/go-chi/chi/v5"
)

// POST /api/auth/login  body: {"username":"...","password":"..."}
func (s *server) authLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	tok, u, err := auth.Login(s.cfg.DB, req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.audit(audit.Event{Type: "auth.login", Actor: req.Username, Action: "login", Outcome: "failure", IP: clientIP(r)})
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(audit.Event{Type: "auth.login", Actor: req.Username, Action: "login", Outcome: "success", IP: clientIP(r)})
	writeJSON(w, 200, map[string]any{"token": tok, "user": u})
}

// POST /api/auth/logout
func (s *server) authLogout(w http.ResponseWriter, r *http.Request) {
	tok := auth.ExtractToken(r)
	if tok != "" {
		_ = auth.Logout(s.cfg.DB, tok)
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/auth/me
func (s *server) authMe(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, 200, u)
}

// POST /api/auth/change-password  body: {"current":"...","next":"..."}
func (s *server) authChangePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := auth.ChangePassword(s.cfg.DB, u.ID, req.Current, req.Next, false); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			http.Error(w, "current password incorrect", http.StatusUnauthorized)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/users
func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	list, err := auth.ListUsers(s.cfg.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, list)
}

// POST /api/users  body: {"username":"...","password":"...","role":"admin|operator|viewer"}
func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u, err := auth.CreateUser(s.cfg.DB, req.Username, req.Password, auth.Role(req.Role))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, 200, u)
}

// PATCH /api/users/{id}/role  body: {"role":"..."}
func (s *server) updateUserRole(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := auth.SetRole(s.cfg.DB, id, auth.Role(req.Role)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/users/{id}/password  body: {"next":"..."}  (admin reset, force)
func (s *server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var req struct {
		Next string `json:"next"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := auth.ChangePassword(s.cfg.DB, id, "", req.Next, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/users/{id}
func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	caller, _ := auth.FromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if caller != nil && caller.ID == id {
		http.Error(w, "cannot delete yourself", http.StatusBadRequest)
		return
	}
	if err := auth.DeleteUser(s.cfg.DB, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// helpers ---------------------------------------------------------

func (s *server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	u, ok := auth.FromContext(r.Context())
	if !ok || !u.Role.CanAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s *server) requireWriter(w http.ResponseWriter, r *http.Request) bool {
	u, ok := auth.FromContext(r.Context())
	if !ok || !u.Role.CanWrite() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}
