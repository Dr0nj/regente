package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// schedulePreview — POST /api/schedule/preview
//
// Body: { "definition": <JobDefinition>, "from": "YYYY-MM-DD", "to": "YYYY-MM-DD" }
// Resposta: { "dates": ["YYYY-MM-DD", ...] } — os dias em que a definition RODARIA
// no intervalo, pela MESMA regra da daily (Scheduler.SchedulePreview → IsScheduledOn).
//
// Read-only (não materializa nada). Serve o calendário-preview da aba Schedule:
// dia presente = roda sem falta, ausente = não roda. from/to default = o ano atual.
func (s *server) schedulePreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Definition domain.JobDefinition `json:"definition"`
		From       string               `json:"from"`
		To         string               `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}

	now := time.Now()
	parse := func(v string, def time.Time) time.Time {
		if v == "" {
			return def
		}
		t, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			return def
		}
		return t
	}
	from := parse(body.From, time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.Local))
	to := parse(body.To, time.Date(now.Year(), 12, 31, 0, 0, 0, 0, time.Local))
	if to.Before(from) {
		from, to = to, from
	}

	dates := s.cfg.Scheduler.SchedulePreview(body.Definition, from, to)
	writeJSON(w, http.StatusOK, map[string]any{
		"from":  from.Format("2006-01-02"),
		"to":    to.Format("2006-01-02"),
		"dates": dates,
	})
}
