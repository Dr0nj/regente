package api

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type instanceRow struct {
	ID           string     `json:"id"`
	DefinitionID string     `json:"definitionId"`
	OrderDate    string     `json:"orderDate"`
	Status       string     `json:"status"`
	ScheduledAt  time.Time  `json:"scheduledAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	AgentID      string     `json:"agentId,omitempty"`
	ExitCode     int        `json:"exitCode,omitempty"`
	Output       string     `json:"output,omitempty"`
	Forced       bool       `json:"forced,omitempty"`
}

func (s *server) listInstances(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	rows, err := s.cfg.DB.Query(
		`SELECT id, definition_id, order_date, status, scheduled_at,
		        started_at, finished_at,
		        COALESCE(agent_id,''), COALESCE(exit_code,0), COALESCE(output,''), COALESCE(forced,0)
		 FROM instances WHERE order_date=? ORDER BY scheduled_at`, date,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []instanceRow{}
	for rows.Next() {
		var ir instanceRow
		var startedAt, finishedAt sql.NullTime
		var forcedInt int
		if err := rows.Scan(
			&ir.ID, &ir.DefinitionID, &ir.OrderDate, &ir.Status, &ir.ScheduledAt,
			&startedAt, &finishedAt,
			&ir.AgentID, &ir.ExitCode, &ir.Output, &forcedInt,
		); err != nil {
			continue
		}
		if startedAt.Valid {
			t := startedAt.Time
			ir.StartedAt = &t
		}
		if finishedAt.Valid {
			t := finishedAt.Time
			ir.FinishedAt = &t
		}
		ir.Forced = forcedInt == 1
		out = append(out, ir)
	}
	// F11.10b — filtra instances cuja folder o user nao pode ler.
	if u, ok := auth.FromContext(r.Context()); ok && u != nil && u.Role != auth.RoleAdmin {
		defs, _ := s.cfg.Store.List()
		defTeam := map[string]string{}
		for _, d := range defs {
			defTeam[d.ID] = d.Team
		}
		filtered := out[:0]
		for _, ir := range out {
			team := defTeam[ir.DefinitionID]
			if team == "" {
				continue // orphan, hide from non-admin
			}
			canRead, _ := auth.CanReadFolder(s.cfg.DB, u, team)
			if canRead {
				filtered = append(filtered, ir)
			}
		}
		out = filtered
	}
	writeJSON(w, 200, out)
}

func (s *server) holdInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	_, err := s.cfg.DB.Exec(`UPDATE instances SET status=? WHERE id=? AND status=?`,
		string(domain.StatusHeld), id, string(domain.StatusWaiting))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "held", "operator", "")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusHeld)})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusHeld)})
}

func (s *server) releaseInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	_, err := s.cfg.DB.Exec(`UPDATE instances SET status=? WHERE id=? AND status=?`,
		string(domain.StatusWaiting), id, string(domain.StatusHeld))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "released", "operator", "")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusWaiting)})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusWaiting)})
}

func (s *server) cancelInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	_, err := s.cfg.DB.Exec(`UPDATE instances SET status=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`,
		string(domain.StatusCancelled), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "cancelled", "operator", "manual cancel")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusCancelled)})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusCancelled)})
}

func (s *server) rerunInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	_, err := s.cfg.DB.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL WHERE id=?`,
		string(domain.StatusWaiting), id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "rerun", "operator", "reset to WAITING")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusWaiting)})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusWaiting)})
}

// setOKInstance — Control-M "Set OK": flip NOTOK/CANCELLED -> OK,
// destrava sucessores on-success.
func (s *server) setOKInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	if err := s.cfg.Scheduler.SetOK(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusOK)})
}

func (s *server) runDaily(w http.ResponseWriter, r *http.Request) {
	today := time.Now().Format("2006-01-02")
	created := s.cfg.Scheduler.RunDaily(today)
	writeJSON(w, 200, map[string]interface{}{"orderDate": today, "created": created})
}

// schedulerTick — Fase 1 (serverless): dispara um ciclo de scheduling sob
// demanda. Pensado para -scheduler=external, onde um cron externo (Cloud
// Scheduler, k8s CronJob, GitHub Actions) bate aqui em vez do ticker interno.
// Idempotente e leader-guarded; seguro chamar com frequência.
func (s *server) schedulerTick(w http.ResponseWriter, r *http.Request) {
	s.cfg.Scheduler.Tick()
	writeJSON(w, 200, map[string]interface{}{"ok": true, "at": time.Now().UTC().Format(time.RFC3339)})
}

func (s *server) forceOrder(w http.ResponseWriter, r *http.Request) {
	defID := chi.URLParam(r, "id")
	// F11.10b — precisa de write na folder da definition.
	defs, err := s.cfg.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var team string
	for _, d := range defs {
		if d.ID == defID {
			team = d.Team
			break
		}
	}
	if team == "" {
		http.Error(w, "definition not found", http.StatusNotFound)
		return
	}
	if !s.requireFolderWrite(w, r, team) {
		return
	}
	id, err := s.cfg.Scheduler.ForceOrder(defID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, 200, map[string]string{"instanceId": id})
}

func (s *server) listAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.cfg.Hub.OnlineAgents())
}

type instanceEvent struct {
	ID         int64     `json:"id"`
	InstanceID string    `json:"instanceId"`
	TS         time.Time `json:"ts"`
	Kind       string    `json:"kind"`
	Actor      string    `json:"actor,omitempty"`
	Message    string    `json:"message,omitempty"`
}

func (s *server) listInstanceEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := s.cfg.DB.Query(
		`SELECT id, instance_id, ts, kind, COALESCE(actor,''), COALESCE(message,'')
		 FROM instance_events WHERE instance_id=? ORDER BY id DESC`,
		id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []instanceEvent{}
	for rows.Next() {
		var e instanceEvent
		if err := rows.Scan(&e.ID, &e.InstanceID, &e.TS, &e.Kind, &e.Actor, &e.Message); err != nil {
			continue
		}
		out = append(out, e)
	}
	writeJSON(w, 200, out)
}
