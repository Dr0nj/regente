// Package api — F11.8 Find & Update (Mass Update / ações em lote).
//
// Dois alvos, respeitando o modelo de produto (memory/core/regente-product-model.md):
//
//	POST /api/bulk/instances                 → Monitoring: hold/release/cancel/rerun/set-ok/confirm/delete em N instances
//	POST /api/design/sessions/{sid}/bulk     → Design: move-folder/patch/delete em N definitions DA SESSION
//
// Transacional POR ITEM: falha parcial é reportada item a item, não aborta o lote.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
)

// bulkItemResult — resultado por item do lote.
type bulkItemResult struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

type bulkResponse struct {
	Action  string           `json:"action"`
	Total   int              `json:"total"`
	Ok      int              `json:"ok"`
	Failed  int              `json:"failed"`
	Results []bulkItemResult `json:"results"`
}

func newBulkResponse(action string, results []bulkItemResult) bulkResponse {
	resp := bulkResponse{Action: action, Total: len(results), Results: results}
	for _, r := range results {
		if r.OK {
			resp.Ok++
		} else {
			resp.Failed++
		}
	}
	return resp
}

// ───────────────────────────────────────────────────────────────
// Monitoring — POST /api/bulk/instances
// body: {"action": "hold|release|cancel|rerun|set-ok|confirm|delete", "ids": ["..."]}
// ───────────────────────────────────────────────────────────────

func (s *server) bulkInstances(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		http.Error(w, "ids is empty", http.StatusBadRequest)
		return
	}
	if len(req.IDs) > 500 {
		http.Error(w, "too many ids (max 500)", http.StatusBadRequest)
		return
	}
	switch req.Action {
	case "hold", "release", "cancel", "rerun", "set-ok", "confirm", "delete":
	default:
		http.Error(w, "unknown action (expected hold|release|cancel|rerun|set-ok|confirm|delete)", http.StatusBadRequest)
		return
	}

	actor := actorFromCtx(r)
	results := make([]bulkItemResult, 0, len(req.IDs))
	for _, id := range req.IDs {
		if err := s.canWriteInstanceQuiet(r, id); err != nil {
			results = append(results, bulkItemResult{ID: id, Error: err.Error()})
			continue
		}
		status, err := s.applyInstanceAction(actor, id, req.Action)
		if err != nil {
			results = append(results, bulkItemResult{ID: id, Error: err.Error()})
			continue
		}
		results = append(results, bulkItemResult{ID: id, OK: true, Status: status})
	}
	resp := newBulkResponse(req.Action, results)
	s.cfg.Hub.BroadcastWeb("instance.bulk", map[string]any{
		"action": req.Action, "total": resp.Total, "ok": resp.Ok, "failed": resp.Failed, "actor": actor,
	})
	// Ações que podem deixar jobs elegíveis JÁ (confirm destrava WAIT_CONFIRM;
	// rerun/release re-entram no gating; set-ok libera dependentes; delete pode
	// devolver eventos ao pool e destravar um clone em WAIT EVENT) cutucam o
	// tick uma vez — os afetados entram na hora, sem esperar o próximo ciclo.
	switch req.Action {
	case "confirm", "rerun", "release", "set-ok", "delete":
		if resp.Ok > 0 {
			go s.cfg.Scheduler.Tick()
		}
	}
	writeJSON(w, 200, resp)
}

// canWriteInstanceQuiet — variante de requireInstanceWrite que retorna erro em
// vez de escrever a resposta HTTP (necessário para feedback por item no bulk:
// E3 manda reportar 403 POR ITEM, não abortar o lote). Mesma semântica do
// unitário: folder da coluna `team` da instance (fallback def viva); job solto
// (team='') passa pelo CanWriteFolder("") — admin/operator irrestrito sim,
// user em modo ACL-restrito não.
func (s *server) canWriteInstanceQuiet(r *http.Request, instanceID string) error {
	folder, err := s.instanceFolder(instanceID)
	if err != nil {
		return fmt.Errorf("instance not found")
	}
	u, ok := auth.FromContext(r.Context())
	if !ok || u == nil {
		return fmt.Errorf("unauthorized")
	}
	can, err := auth.CanWriteFolder(s.cfg.DB, u, folder)
	if err != nil {
		return err
	}
	if !can {
		if folder == "" {
			return fmt.Errorf("no write access (instance sem folder)")
		}
		return fmt.Errorf("no write access to folder %s", folder)
	}
	return nil
}

// applyInstanceAction — mesma semântica dos handlers unitários, com checagem
// de rows-affected para feedback honesto (ex.: hold em instance que não está
// WAITING reporta erro do item em vez de "ok" silencioso).
func (s *server) applyInstanceAction(actor, id, action string) (string, error) {
	switch action {
	case "hold":
		// Hold geral (schemaV16): qualquer status exceto RUNNING (execução já no
		// agente) e o próprio HELD; held_from_status congela o original — o
		// release restaura ELE (ver holdInstance).
		res, err := s.cfg.DB.Exec(
			`UPDATE instances SET held_from_status=status, status=?, hold_scope='' WHERE id=? AND status NOT IN (?,?)`,
			string(domain.StatusHeld), id, string(domain.StatusRunning), string(domain.StatusHeld))
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "", fmt.Errorf("not holdable (RUNNING ou já em HOLD)")
		}
		var heldFrom string
		_ = s.cfg.DB.QueryRow(`SELECT COALESCE(held_from_status,'') FROM instances WHERE id=?`, id).Scan(&heldFrom)
		s.cfg.Scheduler.EmitEvent(id, "held", actor, "bulk")
		s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusHeld), "holdScope": "", "heldFromStatus": heldFrom})
		return string(domain.StatusHeld), nil

	case "release":
		// Restaura o status congelado pelo hold (releaseSQL). Mesma regra do
		// unitário: hold de folder NÃO libera 1-a-1 (só o resume da folder).
		res, err := s.cfg.DB.Exec(releaseSQL+` WHERE id=? AND status=? AND hold_scope=''`,
			id, string(domain.StatusHeld))
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "", fmt.Errorf("not in HELD individual (release skipped — pausa de folder libera pela folder)")
		}
		status := string(domain.StatusWaiting)
		_ = s.cfg.DB.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status)
		s.cfg.Scheduler.EmitEvent(id, "released", actor, "bulk")
		s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": status, "holdScope": "", "heldFromStatus": ""})
		return status, nil

	case "cancel":
		// Cancel não toca o pool de condições (ver cancelInstance).
		res, err := s.cfg.DB.Exec(`UPDATE instances SET status=?, held_from_status='', finished_at=CURRENT_TIMESTAMP WHERE id=?`,
			string(domain.StatusCancelled), id)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "", fmt.Errorf("instance not found")
		}
		s.cfg.Scheduler.EmitEvent(id, "cancelled", actor, "bulk cancel")
		s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusCancelled)})
		return string(domain.StatusCancelled), nil

	case "delete":
		// Control-M "Delete job" (ver deleteInstance): só em HOLD — RUNNING nunca
		// é deletável (não é segurável). O pool de condições fica intacto.
		var status string
		if err := s.cfg.DB.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status); err != nil {
			return "", fmt.Errorf("instance not found")
		}
		if status != string(domain.StatusHeld) {
			return "", fmt.Errorf("delete exige HOLD (status atual: %s)", status)
		}
		if _, err := s.cfg.DB.Exec(`DELETE FROM instance_events WHERE instance_id=?`, id); err != nil {
			return "", err
		}
		res, err := s.cfg.DB.Exec(`DELETE FROM instances WHERE id=? AND status=?`, id, string(domain.StatusHeld))
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "", fmt.Errorf("instance mudou de estado durante o delete")
		}
		s.cfg.Hub.BroadcastWeb("instance.deleted", map[string]string{"id": id})
		return "deleted", nil

	case "rerun":
		// Rerun não toca o pool: o gate re-avalia o que existe (rerun após OK
		// espera — o OK consumiu a entrada via OutRemove; ver rerunInstance).
		//
		// BUG-2: `confirmed` sobrevive ao rerun (ver rerunInstance) — job já
		// confirmado não volta pro gate CONFIRM. Bypass do Run Now não é
		// pegajoso (ver rerunInstance): forced zera quando mode='' .
		res, err := s.cfg.DB.Exec(
			`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL,
			        held_from_status='',
			        forced = CASE WHEN COALESCE(force_mode,'')='' THEN 0 ELSE forced END
			 WHERE id=?`,
			string(domain.StatusWaiting), id,
		)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "", fmt.Errorf("instance not found")
		}
		s.cfg.Scheduler.EmitEvent(id, "rerun", actor, "bulk rerun")
		s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusWaiting)})
		return string(domain.StatusWaiting), nil

	case "set-ok":
		if err := s.cfg.Scheduler.SetOK(id); err != nil {
			return "", err
		}
		return string(domain.StatusOK), nil

	case "confirm":
		res, err := s.cfg.DB.Exec(`UPDATE instances SET confirmed=1 WHERE id=? AND status IN (?,?)`,
			id, string(domain.StatusWaiting), string(domain.StatusHeld))
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "", fmt.Errorf("not in WAITING/HELD (confirm skipped)")
		}
		s.cfg.Scheduler.EmitEvent(id, "confirmed", actor, "bulk confirm")
		s.cfg.Hub.BroadcastWeb("instance.changed", map[string]interface{}{"id": id, "confirmed": true})
		return "confirmed", nil
	}
	return "", fmt.Errorf("unknown action")
}

// ───────────────────────────────────────────────────────────────
// Design — POST /api/design/sessions/{sid}/bulk
// body: {"action": "move-folder|patch|delete", "ids": [...],
//        "targetFolder": "...", "patch": {retries?, timeout?, agentId?, calendar?}}
// Opera no clone DA SESSION (write-staging local; nada vai pro GitHub até Publish).
// ───────────────────────────────────────────────────────────────

type bulkDefinitionPatch struct {
	Retries  *int    `json:"retries,omitempty"`
	Timeout  *int    `json:"timeout,omitempty"`
	AgentID  *string `json:"agentId,omitempty"`
	Calendar *string `json:"calendar,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"` // schedule.enabled em lote (Find & Update)
	RunAt    *string `json:"runAt,omitempty"`   // schedule.runAt em lote ("" limpa)
}

func (s *server) bulkSessionDefinitions(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		Action       string              `json:"action"`
		IDs          []string            `json:"ids"`
		TargetFolder string              `json:"targetFolder,omitempty"`
		Patch        bulkDefinitionPatch `json:"patch,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		http.Error(w, "ids is empty", http.StatusBadRequest)
		return
	}
	if len(req.IDs) > 500 {
		http.Error(w, "too many ids (max 500)", http.StatusBadRequest)
		return
	}
	switch req.Action {
	case "move-folder":
		if req.TargetFolder == "" {
			http.Error(w, "targetFolder required for move-folder", http.StatusBadRequest)
			return
		}
	case "patch", "delete":
	default:
		http.Error(w, "unknown action (expected move-folder|patch|delete)", http.StatusBadRequest)
		return
	}

	u, _ := auth.FromContext(r.Context())
	// move precisa de write no destino além da origem.
	if req.Action == "move-folder" && u != nil {
		if can, _ := auth.CanWriteFolder(s.cfg.DB, u, req.TargetFolder); !can {
			http.Error(w, "forbidden: no write on target folder "+req.TargetFolder, http.StatusForbidden)
			return
		}
	}

	defs, err := sess.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	byID := make(map[string]domain.JobDefinition, len(defs))
	for _, d := range defs {
		byID[d.ID] = d
	}

	results := make([]bulkItemResult, 0, len(req.IDs))
	for _, id := range req.IDs {
		def, found := byID[id]
		if !found {
			results = append(results, bulkItemResult{ID: id, Error: "definition not found in session"})
			continue
		}
		if u != nil {
			if can, _ := auth.CanWriteFolder(s.cfg.DB, u, def.Team); !can {
				results = append(results, bulkItemResult{ID: id, Error: "no write access to folder " + def.Team})
				continue
			}
		}
		switch req.Action {
		case "delete":
			if err := sess.Store.Delete(def.Team, def.ID); err != nil {
				results = append(results, bulkItemResult{ID: id, Error: err.Error()})
				continue
			}
			results = append(results, bulkItemResult{ID: id, OK: true, Status: "deleted"})

		case "move-folder":
			oldTeam := def.Team
			if oldTeam == req.TargetFolder {
				results = append(results, bulkItemResult{ID: id, OK: true, Status: "already in folder"})
				continue
			}
			def.Team = req.TargetFolder
			if err := sess.Store.Save(def); err != nil {
				results = append(results, bulkItemResult{ID: id, Error: "save in target: " + err.Error()})
				continue
			}
			if err := sess.Store.Delete(oldTeam, def.ID); err != nil {
				results = append(results, bulkItemResult{ID: id, Error: "saved in target but failed removing from " + oldTeam + ": " + err.Error()})
				continue
			}
			results = append(results, bulkItemResult{ID: id, OK: true, Status: "moved to " + req.TargetFolder})

		case "patch":
			if req.Patch.Retries != nil {
				def.Retries = *req.Patch.Retries
			}
			if req.Patch.Timeout != nil {
				def.Timeout = *req.Patch.Timeout
			}
			if req.Patch.AgentID != nil {
				def.AgentID = *req.Patch.AgentID
			}
			if req.Patch.Calendar != nil {
				def.Calendar = *req.Patch.Calendar
			}
			if req.Patch.Enabled != nil {
				def.Schedule.Enabled = *req.Patch.Enabled
			}
			if req.Patch.RunAt != nil {
				def.Schedule.RunAt = *req.Patch.RunAt
			}
			if err := domain.ValidateDefinitionDraft(def); err != nil {
				results = append(results, bulkItemResult{ID: id, Error: err.Error()})
				continue
			}
			if err := sess.Store.Save(def); err != nil {
				results = append(results, bulkItemResult{ID: id, Error: err.Error()})
				continue
			}
			results = append(results, bulkItemResult{ID: id, OK: true, Status: "patched"})
		}
	}

	s.cfg.Sessions.PersistSession(sess)
	writeJSON(w, 200, newBulkResponse(req.Action, results))
}
