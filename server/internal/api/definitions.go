package api

import (
	"encoding/json"
	"net/http"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/go-chi/chi/v5"
)

func (s *server) listDefinitions(w http.ResponseWriter, r *http.Request) {
	defs, err := s.cfg.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// F11.10b — filtra por ACL de leitura.
	if u, ok := auth.FromContext(r.Context()); ok && u != nil {
		filtered := defs[:0]
		for _, d := range defs {
			canRead, _ := auth.CanReadFolder(s.cfg.DB, u, d.Team)
			if canRead {
				filtered = append(filtered, d)
			}
		}
		defs = filtered
	}
	writeJSON(w, 200, defs)
}

func (s *server) saveDefinition(w http.ResponseWriter, r *http.Request) {
	var def domain.JobDefinition
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// F11.10b — precisa de write na folder destino.
	if !s.requireFolderWrite(w, r, def.Team) {
		return
	}
	// F12 — validação por jobType.
	if err := validateDefinition(def); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// F13.2 — write-side PR mode.
	actor := actorFromCtx(r)
	pr, err := s.applyWrite(actor,
		def.Team+"-"+def.ID,
		"regente: save "+def.Team+"/"+def.ID,
		"["+actor+"] save "+def.Team+"/"+def.ID,
		"Automated PR by Regente. Save of definition `"+def.Team+"/"+def.ID+"` by `"+actor+"`.",
		func() error { return s.cfg.Store.Save(def) })
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.recordDefinitionAudit(actor, "save", def.Team, def.ID, pr)
	s.cfg.Scheduler.ReloadDefs()
	s.cfg.Hub.BroadcastWeb("definition.changed", def)
	writeJSON(w, 200, map[string]any{"definition": def, "git": pr})
}

func (s *server) deleteDefinition(w http.ResponseWriter, r *http.Request) {
	team := chi.URLParam(r, "team")
	id := chi.URLParam(r, "id")
	if !s.requireFolderWrite(w, r, team) {
		return
	}
	actor := actorFromCtx(r)
	pr, err := s.applyWrite(actor,
		team+"-"+id+"-delete",
		"regente: delete "+team+"/"+id,
		"["+actor+"] delete "+team+"/"+id,
		"Automated PR by Regente. Delete of `"+team+"/"+id+"` by `"+actor+"`.",
		func() error { return s.cfg.Store.Delete(team, id) })
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.recordDefinitionAudit(actor, "delete", team, id, pr)
	s.cfg.Scheduler.ReloadDefs()
	s.cfg.Hub.BroadcastWeb("definition.deleted", map[string]any{"team": team, "id": id, "git": pr})
	writeJSON(w, 200, map[string]any{"deleted": true, "git": pr})
}

func (s *server) listFolders(w http.ResponseWriter, r *http.Request) {
	f, err := s.cfg.Store.ListFolders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// F11.10b — filtra por ACL.
	if u, ok := auth.FromContext(r.Context()); ok && u != nil {
		filtered := make([]storage.FolderInfo, 0, len(f))
		for _, fi := range f {
			canRead, _ := auth.CanReadFolder(s.cfg.DB, u, fi.Name)
			if canRead {
				filtered = append(filtered, fi)
			}
		}
		f = filtered
	}
	writeJSON(w, 200, f)
}

func (s *server) createFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// F11.10b — criar folder requer ser admin (cria namespace novo).
	if u, ok := auth.FromContext(r.Context()); !ok || u == nil || !u.Role.CanAdmin() {
		http.Error(w, "forbidden: admin only", http.StatusForbidden)
		return
	}
	if err := s.cfg.Store.CreateFolder(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.directPush("create folder "+req.Name, actorFromCtx(r))
	s.cfg.Hub.BroadcastWeb("folder.changed", map[string]string{"name": req.Name, "action": "created"})
	writeJSON(w, 200, map[string]string{"name": req.Name})
}

// renameFolder — PATCH /api/folders/{name}  body: {"newName": "..."}
func (s *server) renameFolder(w http.ResponseWriter, r *http.Request) {
	old := chi.URLParam(r, "name")
	var req struct {
		NewName string `json:"newName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireFolderWrite(w, r, old) {
		return
	}
	if err := s.cfg.Store.RenameFolder(old, req.NewName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.directPush("rename folder "+old+" → "+req.NewName, actorFromCtx(r))
	s.cfg.Scheduler.ReloadDefs()
	s.cfg.Hub.BroadcastWeb("folder.changed", map[string]string{"name": req.NewName, "oldName": old, "action": "renamed"})
	writeJSON(w, 200, map[string]string{"name": req.NewName})
}

// deleteFolder — DELETE /api/folders/{name}?force=true|false
func (s *server) deleteFolder(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	force := r.URL.Query().Get("force") == "true"
	if !s.requireFolderWrite(w, r, name) {
		return
	}
	if err := s.cfg.Store.DeleteFolder(name, force); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.directPush("delete folder "+name, actorFromCtx(r))
	s.cfg.Scheduler.ReloadDefs()
	s.cfg.Hub.BroadcastWeb("folder.changed", map[string]string{"name": name, "action": "deleted"})
	w.WriteHeader(http.StatusNoContent)
}

// archiveFolder — POST /api/folders/{name}/archive
func (s *server) archiveFolder(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !s.requireFolderWrite(w, r, name) {
		return
	}
	if err := s.cfg.Store.ArchiveFolder(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.directPush("archive folder "+name, actorFromCtx(r))
	s.cfg.Scheduler.ReloadDefs()
	s.cfg.Hub.BroadcastWeb("folder.changed", map[string]string{"name": name, "action": "archived"})
	writeJSON(w, 200, map[string]string{"name": name})
}
