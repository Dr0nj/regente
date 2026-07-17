package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/go-chi/chi/v5"
)

// decodeDefinition — decodifica uma JobDefinition do body JSON aceitando o campo
// de parâmetros sob DOIS nomes: `actionConfig` (tag json oficial, o que o front
// manda) E `params` (o nome do YAML — integrações externas mandavam "params" e o
// campo era IGNORADO em silêncio, o job despachava sem comando). actionConfig
// vence quando ambos vierem.
func decodeDefinition(r io.Reader) (domain.JobDefinition, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return domain.JobDefinition{}, err
	}
	var def domain.JobDefinition
	if err := json.Unmarshal(body, &def); err != nil {
		return domain.JobDefinition{}, err
	}
	if def.Params == nil {
		var alias struct {
			Params map[string]interface{} `json:"params"`
		}
		if err := json.Unmarshal(body, &alias); err == nil && alias.Params != nil {
			def.Params = alias.Params
		}
	}
	return def, nil
}

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
	def, err := decodeDefinition(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// F11.10b — precisa de write na folder destino.
	if !s.requireFolderWrite(w, r, def.Team) {
		return
	}
	// F12 — validação por jobType.
	if err := domain.ValidateDefinition(def); err != nil {
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
	old := urlName(r, "name")
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
	name := urlName(r, "name")
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

// setFolderLayout — PUT /api/folders/{name}/layout  body: {"columns":6,"maxRows":20}
// UI-3: override POR FOLDER da grade de jobs soltos do canvas, persistido no
// stub .regente-folder.yaml (viaja com o workspace via Git). Campo 0/ausente
// herda o global da UI; body com ambos zerados (ou {}) REMOVE o override.
func (s *server) setFolderLayout(w http.ResponseWriter, r *http.Request) {
	name := urlName(r, "name")
	if !s.requireFolderWrite(w, r, name) {
		return
	}
	var req storage.FolderLayout
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Ranges da UI (SettingsDialog usa 1–40 / 1–200); negativo é bug do caller.
	if req.Columns < 0 || req.Columns > 40 || req.MaxRows < 0 || req.MaxRows > 200 {
		http.Error(w, "layout out of range (columns 0-40, maxRows 0-200; 0 = inherit)", http.StatusBadRequest)
		return
	}
	var lay *storage.FolderLayout
	if req.Columns > 0 || req.MaxRows > 0 {
		lay = &req
	}
	if err := s.cfg.Store.SetFolderLayout(name, lay); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.directPush("layout folder "+name, actorFromCtx(r))
	s.cfg.Hub.BroadcastWeb("folder.changed", map[string]string{"name": name, "action": "layout"})
	writeJSON(w, 200, map[string]any{"name": name, "layout": lay})
}

// archiveFolder — POST /api/folders/{name}/archive
func (s *server) archiveFolder(w http.ResponseWriter, r *http.Request) {
	name := urlName(r, "name")
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
