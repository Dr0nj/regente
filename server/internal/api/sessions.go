// Package api — Design session endpoints (Etapa 3+4+5 do realinhamento, 2026-04-26).
//
// Modelo:
//   - Cliente entra em Design → escolhe folders (existentes + novos) →
//     POST /api/design/sessions cria um clone Git efêmero em sessions/<sid>/.
//   - Todo CRUD de definitions/folders durante a sessão usa
//     /api/design/sessions/{sid}/* — escreve LOCAL no clone, sem push.
//   - POST /api/design/sessions/{sid}/publish faz commit+push pro Git.
//     Se a sessão tem newFolders, força PR (Etapa 5).
//
// Sessions são in-memory. Restart do server perde sessions abertas — usuário
// precisa reabrir. Aceito como tradeoff MVP (registro: memory/decisions/...).
package api

import (
	"encoding/json"
	"net/http"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/go-chi/chi/v5"
)

// === Helpers ===

func (s *server) sessionFromURL(w http.ResponseWriter, r *http.Request) (*storage.DesignSession, bool) {
	if s.cfg.Sessions == nil {
		http.Error(w, "design sessions not configured (no GitOps)", http.StatusServiceUnavailable)
		return nil, false
	}
	sid := chi.URLParam(r, "sid")
	sess, ok := s.cfg.Sessions.Get(sid)
	if !ok {
		http.Error(w, "session not found (may have expired on server restart)", http.StatusNotFound)
		return nil, false
	}
	return sess, true
}

// === Lifecycle ===

// POST /api/design/sessions — cria session
// body: {"folders": ["FIN", "asd"], "newFolders": ["new1"]}
func (s *server) createDesignSession(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Sessions == nil {
		http.Error(w, "design sessions not configured (no GitOps)", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Folders    []string `json:"folders"`
		NewFolders []string `json:"newFolders"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	actor := actorFromCtx(r)
	// Folder ACL: usuário precisa de write em cada folder (existente ou novo).
	if u, ok := auth.FromContext(r.Context()); ok && u != nil {
		for _, f := range req.Folders {
			if can, _ := auth.CanWriteFolder(s.cfg.DB, u, f); !can {
				http.Error(w, "forbidden: no write on folder "+f, http.StatusForbidden)
				return
			}
		}
		// criação de folder nova → admin-only (consistente com createFolder global).
		if len(req.NewFolders) > 0 && !u.Role.CanAdmin() {
			http.Error(w, "forbidden: creating new folder requires admin", http.StatusForbidden)
			return
		}
	}
	sess, err := s.cfg.Sessions.Create(actor, req.Folders, req.NewFolders)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, 201, sess)
}

// GET /api/design/sessions — lista sessions do actor (admin vê todas).
func (s *server) listDesignSessions(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Sessions == nil {
		writeJSON(w, 200, []any{})
		return
	}
	filter := actorFromCtx(r)
	if u, ok := auth.FromContext(r.Context()); ok && u != nil && u.Role.CanAdmin() {
		filter = ""
	}
	writeJSON(w, 200, s.cfg.Sessions.List(filter))
}

// GET /api/design/sessions/{sid}
func (s *server) getDesignSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, sess)
}

// GET /api/design/sessions/{sid}/status
// Retorna ahead/behind do clone da session vs origin/<branch>.
// P8 (2026-04-26) — badge de drift por session.
func (s *server) getDesignSessionStatus(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	ahead, behind, err := sess.Git.AheadBehind()
	if err != nil {
		writeJSON(w, 200, map[string]any{"ahead": 0, "behind": 0, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ahead": ahead, "behind": behind})
}

// DELETE /api/design/sessions/{sid}
func (s *server) deleteDesignSession(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Sessions == nil {
		http.Error(w, "not configured", http.StatusServiceUnavailable)
		return
	}
	sid := chi.URLParam(r, "sid")
	if err := s.cfg.Sessions.Delete(sid); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// === Read endpoints (folders/definitions in session workspace) ===

// GET /api/design/sessions/{sid}/folders
func (s *server) listSessionFolders(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	f, err := sess.Store.ListFolders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, f)
}

// GET /api/design/sessions/{sid}/definitions
func (s *server) listSessionDefinitions(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	defs, err := sess.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, defs)
}

// === Write endpoints (mutate session workspace; NO push — Etapa 4) ===

// POST /api/design/sessions/{sid}/definitions
func (s *server) saveSessionDefinition(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	var def domain.JobDefinition
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireFolderWrite(w, r, def.Team) {
		return
	}
	if err := validateDefinition(def); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := sess.Store.Save(def); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, map[string]any{"definition": def, "session": sess.ID})
}

// DELETE /api/design/sessions/{sid}/definitions/{team}/{id}
func (s *server) deleteSessionDefinition(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	team := chi.URLParam(r, "team")
	id := chi.URLParam(r, "id")
	if !s.requireFolderWrite(w, r, team) {
		return
	}
	if err := sess.Store.Delete(team, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true, "session": sess.ID})
}

// POST /api/design/sessions/{sid}/folders
// body: {"name": "..."}
// Marca como newFolder na session → Publish forçará PR (Etapa 5).
func (s *server) createSessionFolder(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if u, ok := auth.FromContext(r.Context()); !ok || u == nil || !u.Role.CanAdmin() {
		http.Error(w, "forbidden: admin only", http.StatusForbidden)
		return
	}
	if err := sess.Store.CreateFolder(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sess.AddNewFolder(req.Name)
	s.cfg.Sessions.PersistSession(sess) // P6 — persiste mudança no DB
	writeJSON(w, 201, map[string]any{"name": req.Name, "session": sess.ID, "willForcePR": true})
}

// POST /api/design/sessions/{sid}/folders/open
// body: {"name": "..."}
// Adiciona uma folder EXISTENTE no escopo da session (sess.Folders).
// Não cria diretório, não força PR. Re-alinhamento Design Fase 2 (2026-04-27).
func (s *server) openSessionFolder(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if u, ok := auth.FromContext(r.Context()); !ok || u == nil || !u.Role.CanAdmin() {
		http.Error(w, "forbidden: admin only", http.StatusForbidden)
		return
	}
	// Valida que a folder existe no clone (rejeita open de folder fantasma).
	folders, err := sess.Store.ListFolders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	found := false
	for _, f := range folders {
		if f.Name == req.Name {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "folder not found in session workspace", http.StatusNotFound)
		return
	}
	sess.AddFolder(req.Name)
	s.cfg.Sessions.PersistSession(sess)
	writeJSON(w, 200, map[string]any{"name": req.Name, "session": sess.ID, "willForcePR": false})
}

// === Publish ===

// POST /api/design/sessions/{sid}/publish
// body: {"message": "..."}
func (s *server) publishDesignSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	res, err := s.cfg.Sessions.Publish(sess.ID, req.Message, s.cfg.WriteMode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// P4 (2026-04-26) — empty publish: nada para fazer, mantém session viva
	// para o usuário continuar editando se quiser.
	if res.Mode == "noop" {
		writeJSON(w, 200, res)
		return
	}
	// Audit cada folder da sessão.
	actor := actorFromCtx(r)
	pr := &storage.PRResult{
		PRNumber: res.PRNumber, PRURL: res.PRURL,
		Branch: res.Branch, CommitSHA: res.CommitSHA, Mode: res.Mode,
	}
	for _, f := range append(sess.Folders, sess.NewFolders...) {
		s.recordDefinitionAudit(actor, "publish", f, "session-"+sess.ID, pr)
	}
	// Sync workspace principal pra refletir o que foi publicado (se direct push).
	if res.Mode == string(storage.WriteModeDirect) && s.cfg.Git != nil {
		if err := s.cfg.Git.SyncFromRemote(); err == nil {
			s.cfg.Scheduler.ReloadDefs()
			s.cfg.Hub.BroadcastWeb("definition.changed", map[string]string{"reason": "design-publish"})
			s.cfg.Hub.BroadcastWeb("folder.changed", map[string]string{"reason": "design-publish"})
		}
	}
	// Limpa session após publish bem-sucedido.
	_ = s.cfg.Sessions.Delete(sess.ID)
	writeJSON(w, 200, res)
}
