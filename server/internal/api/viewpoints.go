// Package api — ViewPoints salvos (Control-M ViewPoint): filtros nomeados do
// Monitoring persistidos no servidor, por usuário.
//
//	GET    /api/viewpoints        → os meus + os compartilhados (shared=1)
//	POST   /api/viewpoints        → cria {name, filters, shared?}
//	DELETE /api/viewpoints/{id}   → dono ou admin
//
// `filters` é um JSON opaco pro servidor (folder/status/busca/preset…) — quem
// interpreta é a UI (ScaleMonitor/Monitoring). Os "dashboards prontos" são
// presets fixos da UI em cima do mesmo shape; aqui só vive o que o usuário
// salvou.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/go-chi/chi/v5"
)

type viewpointRow struct {
	ID        int64           `json:"id"`
	Owner     string          `json:"owner"`
	Name      string          `json:"name"`
	Filters   json.RawMessage `json:"filters"`
	Shared    bool            `json:"shared"`
	CreatedAt time.Time       `json:"createdAt"`
}

func (s *server) listViewpoints(w http.ResponseWriter, r *http.Request) {
	owner := actorFromCtx(r)
	rows, err := s.cfg.DB.Query(
		`SELECT id, owner, name, filters_json, shared, created_at
		 FROM viewpoints WHERE owner=? OR shared=1 ORDER BY name`, owner)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []viewpointRow{}
	for rows.Next() {
		var v viewpointRow
		var filters string
		var sharedInt int
		if rows.Scan(&v.ID, &v.Owner, &v.Name, &filters, &sharedInt, &v.CreatedAt) != nil {
			continue
		}
		v.Filters = json.RawMessage(filters)
		v.Shared = sharedInt == 1
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}

func (s *server) createViewpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string          `json:"name"`
		Filters json.RawMessage `json:"filters"`
		Shared  bool            `json:"shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 80 {
		http.Error(w, "name too long (max 80)", http.StatusBadRequest)
		return
	}
	if len(req.Filters) == 0 {
		req.Filters = json.RawMessage(`{}`)
	} else if !json.Valid(req.Filters) {
		http.Error(w, "filters must be valid JSON", http.StatusBadRequest)
		return
	}
	owner := actorFromCtx(r)
	// upsert por (owner, name): salvar de novo com o mesmo nome ATUALIZA os
	// filtros — é o gesto natural de "salvar viewpoint" repetido na UI.
	var existing int64
	err := s.cfg.DB.QueryRow(`SELECT id FROM viewpoints WHERE owner=? AND name=?`, owner, req.Name).Scan(&existing)
	sharedInt := 0
	if req.Shared {
		sharedInt = 1
	}
	if err == nil {
		if _, err := s.cfg.DB.Exec(`UPDATE viewpoints SET filters_json=?, shared=? WHERE id=?`,
			string(req.Filters), sharedInt, existing); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, 200, map[string]interface{}{"id": existing, "name": req.Name, "updated": true})
		return
	}
	id, err := s.cfg.DB.InsertID(
		`INSERT INTO viewpoints(owner, name, filters_json, shared) VALUES(?,?,?,?)`,
		owner, req.Name, string(req.Filters), sharedInt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, map[string]interface{}{"id": id, "name": req.Name})
}

func (s *server) deleteViewpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var owner string
	if err := s.cfg.DB.QueryRow(`SELECT owner FROM viewpoints WHERE id=?`, id).Scan(&owner); err != nil {
		http.Error(w, "viewpoint not found", http.StatusNotFound)
		return
	}
	u, _ := auth.FromContext(r.Context())
	if owner != actorFromCtx(r) && (u == nil || !u.Role.CanAdmin()) {
		http.Error(w, "forbidden: not the owner", http.StatusForbidden)
		return
	}
	if _, err := s.cfg.DB.Exec(`DELETE FROM viewpoints WHERE id=?`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, map[string]interface{}{"id": id, "deleted": true})
}
