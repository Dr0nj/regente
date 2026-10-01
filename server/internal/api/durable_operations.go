package api

import (
	"github.com/Dr0nj/regente-server/internal/execution"
	"net/http"
)

func (s *server) durableInstanceAction(w http.ResponseWriter, r *http.Request, id, action string) bool {
	if s.cfg.Scheduler.DurableEngine() == nil {
		return false
	}
	scope, err := s.instanceWebScope(id)
	if err != nil {
		executionError(w, execution.ErrNotFound)
		return true
	}
	status, err := s.cfg.Scheduler.DurableAction(actorFromCtx(r), id, action)
	if err != nil {
		executionError(w, err)
		return true
	}
	if action == "delete" {
		s.broadcastWeb("instance.deleted", map[string]any{"id": id, "_scope": scope})
	}
	switch action {
	case "rerun", "set-ok", "release", "confirm", "delete", "force":
		go s.cfg.Scheduler.Tick()
	}
	writeJSON(w, 200, map[string]any{"id": id, "status": status, "confirmed": action == "confirm"})
	return true
}
