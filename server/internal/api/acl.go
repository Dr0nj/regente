package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/go-chi/chi/v5"
)

// ───────────────────────────────────────────────────────────────
// F11.10b — ACL endpoints (admin-only)
// ───────────────────────────────────────────────────────────────
//   GET    /api/users/{id}/acls          → []FolderACL
//   PUT    /api/users/{id}/acls          → body [{folder,perms}]; replace all
//   PATCH  /api/users/{id}/acls/{folder} → body {perms}; perms="" deleta linha
// ───────────────────────────────────────────────────────────────

func (s *server) listUserACLs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	acls, err := auth.ListUserACLs(s.cfg.DB, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, acls)
}

func (s *server) replaceUserACLs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var body []struct {
		Folder string `json:"folder"`
		Perms  string `json:"perms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM folder_acls WHERE user_id=?", id); err != nil {
		_ = tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, e := range body {
		if err := auth.SetUserACL(s.cfg.DB, id, e.Folder, e.Perms); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	out, _ := auth.ListUserACLs(s.cfg.DB, id)
	writeJSON(w, 200, out)
}

func (s *server) setUserACL(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	folder := chi.URLParam(r, "folder")
	var body struct {
		Perms string `json:"perms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := auth.SetUserACL(s.cfg.DB, id, folder, body.Perms); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ───────────────────────────────────────────────────────────────
// Helpers usados pelos handlers existentes para enforce de ACL
// ───────────────────────────────────────────────────────────────

// requireFolderWrite responde 403 e retorna false se o user nao puder mutar `folder`.
func (s *server) requireFolderWrite(w http.ResponseWriter, r *http.Request, folder string) bool {
	u, ok := auth.FromContext(r.Context())
	if !ok || u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	ok, err := auth.CanWriteFolder(s.cfg.DB, u, folder)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	if !ok {
		http.Error(w, "forbidden: no write access to folder", http.StatusForbidden)
		return false
	}
	return true
}

// instanceFolder devolve a folder dona de uma instance. E3 — fonte primária é
// a coluna `team` SNAPSHOTADA na própria instance (v4; barata — 1 SELECT — e
// imutável como o resto do card do Monitoring); só instances pré-v4 sem team
// caem no fallback da definition viva. Ambas vazias = job "solto" (sem folder).
func (s *server) instanceFolder(instanceID string) (string, error) {
	var team, defID string
	if err := s.cfg.DB.QueryRow(
		`SELECT COALESCE(team,''), definition_id FROM instances WHERE id=?`, instanceID,
	).Scan(&team, &defID); err != nil {
		return "", err
	}
	if team != "" || s.cfg.Store == nil {
		return team, nil
	}
	defs, err := s.cfg.Store.List()
	if err != nil {
		return "", err
	}
	for _, d := range defs {
		if d.ID == defID {
			return d.Team, nil
		}
	}
	return "", nil
}

// requireInstanceWrite valida que o user pode mutar a folder dona da instance.
// E3 — enforcement das ações OPERACIONAIS (hold/release/cancel/rerun/set-ok/
// confirm) por folder, além do writer role do middleware: operator com ACL
// write=[FIN] não cancela job do RISCO. Instance sem folder (job solto) passa
// pelo MESMO CanWriteFolder(""): admin e operator irrestrito podem; user em
// modo ACL-restrito não (coerente com o read-path, que nem lista team='' pra
// ele). Bearer legado vira pseudo-admin no middleware → bypassa, por design.
func (s *server) requireInstanceWrite(w http.ResponseWriter, r *http.Request, instanceID string) bool {
	folder, err := s.instanceFolder(instanceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return s.requireFolderWrite(w, r, folder)
}
