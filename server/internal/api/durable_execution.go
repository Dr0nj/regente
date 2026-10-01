package api

import (
	"database/sql"
	"errors"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/go-chi/chi/v5"
	"net/http"
)

type executionEffect struct {
	ID          string `json:"id"`
	ExecutionID string `json:"executionId"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	Reason      string `json:"reason"`
	Generation  int    `json:"generation"`
	CreatedAt   int64  `json:"createdAt"`
	LeaseUntil  int64  `json:"leaseUntil"`
	ResolvedAt  int64  `json:"resolvedAt"`
	ResolvedBy  string `json:"resolvedBy"`
}

func (s *server) executionReadAllowed(w http.ResponseWriter, r *http.Request, id string) bool {
	var team, date string
	if err := s.cfg.DB.QueryRow("SELECT COALESCE(team,''),order_date FROM instances WHERE id=?", id).Scan(&team, &date); err != nil {
		executionError(w, execution.ErrNotFound)
		return false
	}
	if teams, restricted := s.allowedTeams(r, date); restricted {
		for _, allowed := range teams {
			if allowed == team {
				return true
			}
		}
		executionError(w, execution.ErrNotFound)
		return false
	}
	return true
}
func (s *server) instanceExecutions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.executionReadAllowed(w, r, id) {
		return
	}
	attempts := []execution.Attempt{}
	effects := []executionEffect{}
	if s.attempts == nil {
		writeJSON(w, 200, map[string]any{"attempts": attempts, "effects": effects})
		return
	}
	o, err := s.attempts.RuntimeOrder(id)
	if errors.Is(err, execution.ErrNotFound) {
		writeJSON(w, 200, map[string]any{"attempts": attempts, "effects": effects})
		return
	}
	if err != nil {
		executionError(w, err)
		return
	}
	rows, err := s.cfg.DB.Query("SELECT execution_id FROM execution_attempts WHERE order_id=? ORDER BY attempt DESC", o.ID)
	if err != nil {
		executionError(w, err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var exec string
		if err = rows.Scan(&exec); err != nil {
			rows.Close()
			executionError(w, err)
			return
		}
		ids = append(ids, exec)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		executionError(w, err)
		return
	}
	for _, exec := range ids {
		a, err := s.attempts.Attempt(exec)
		if err != nil {
			executionError(w, err)
			return
		}
		attempts = append(attempts, a)
	}
	rows, err = s.cfg.DB.Query("SELECT id,execution_id,kind,state,reason,generation,created_at,lease_until,resolved_at,resolved_by FROM execution_effects WHERE instance_id=? ORDER BY created_at,id", id)
	if err != nil {
		executionError(w, err)
		return
	}
	for rows.Next() {
		var e executionEffect
		if err = rows.Scan(&e.ID, &e.ExecutionID, &e.Kind, &e.State, &e.Reason, &e.Generation, &e.CreatedAt, &e.LeaseUntil, &e.ResolvedAt, &e.ResolvedBy); err != nil {
			rows.Close()
			executionError(w, err)
			return
		}
		effects = append(effects, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"order": o, "attempts": attempts, "effects": effects})
}
func (s *server) resolveExecution(w http.ResponseWriter, r *http.Request) {
	if s.attempts == nil {
		executionError(w, execution.ErrNotFound)
		return
	}
	exec := chi.URLParam(r, "id")
	a, err := s.attempts.Attempt(exec)
	if err != nil {
		executionError(w, err)
		return
	}
	o, err := s.attempts.Order(a.OrderID)
	if err != nil {
		executionError(w, err)
		return
	}
	if !o.Runtime {
		executionError(w, execution.ErrNotFound)
		return
	}
	if !s.requireInstanceWrite(w, r, o.SourceInstanceID) {
		return
	}
	var body struct {
		Fence    int64  `json:"fence"`
		Key      string `json:"idempotencyKey"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
		Stopped  bool   `json:"effectStopped"`
	}
	if !executionBody(w, r, &body) {
		return
	}
	out, err := s.attempts.Resolve(exec, body.Fence, body.Key, actorFromCtx(r), body.Decision, body.Reason, body.Stopped)
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *server) resolveExecutionEffect(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Scheduler.DurableEngine() == nil {
		executionError(w, execution.ErrNotFound)
		return
	}
	id := chi.URLParam(r, "id")
	var instance string
	if err := s.cfg.DB.QueryRow("SELECT instance_id FROM execution_effects WHERE id=?", id).Scan(&instance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = execution.ErrNotFound
		}
		executionError(w, err)
		return
	}
	if !s.requireInstanceWrite(w, r, instance) {
		return
	}
	var body struct {
		Generation     *int   `json:"generation"`
		Key            string `json:"idempotencyKey"`
		Decision       string `json:"decision"`
		Reason         string `json:"reason"`
		Classification string `json:"classification"`
		Stopped        bool   `json:"effectStopped"`
		Risk           bool   `json:"duplicateRiskAccepted"`
	}
	if !executionBody(w, r, &body) {
		return
	}
	if body.Generation == nil {
		executionError(w, execution.ErrInvalid)
		return
	}
	if err := s.cfg.Scheduler.ResolveEffect(id, body.Key, actorFromCtx(r), body.Decision, body.Reason, body.Classification, body.Stopped, body.Risk, *body.Generation); err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "decision": body.Decision})
}
