// Package api — ADV-3: What-If (simulação de cenário) + Statistics por job.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/scheduler"
)

// whatIf — POST /api/whatif {date?, changes:[{defId, delayMin?, durationMs?,
// fail?, skip?}]}. Simulação read-only (nada materializa): projeta a diária
// baseline × cenário com durações REAIS (p50 do histórico, D-4) e devolve o
// impacto downstream. Sem requireWriter de propósito — é consulta, como o
// /forecast; o corpo só existe porque o cenário é estruturado.
func (s *server) whatIf(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Date     string                   `json:"date"`
		Changes  []scheduler.WhatIfChange `json:"changes"`
		Lookback int                      `json:"lookbackDays"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Date == "" {
		req.Date = s.cfg.Scheduler.TodayDate()
	}
	if _, err := time.Parse("2006-01-02", req.Date); err != nil {
		http.Error(w, "date must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	lookback := 14
	if req.Lookback > 0 && req.Lookback <= 90 {
		lookback = req.Lookback
	}

	defs := s.cfg.Scheduler.Defs()
	cals := map[string]*domain.Calendar{}
	if cs := s.cfg.Scheduler.Calendars(); cs != nil {
		list, _ := cs.List()
		for i := range list {
			cals[list[i].Name] = &list[i]
		}
	}
	durations := s.cfg.Scheduler.DayDurations(req.Date, lookback)
	writeJSON(w, 200, scheduler.WhatIf(defs, cals, req.Date, durations, req.Changes))
}

// jobStats — GET /api/analytics/jobstats?defId=… (ADV-3 Statistics).
func (s *server) jobStats(w http.ResponseWriter, r *http.Request) {
	defID := r.URL.Query().Get("defId")
	if defID == "" {
		http.Error(w, "defId required", http.StatusBadRequest)
		return
	}
	writeJSON(w, 200, s.cfg.Scheduler.JobStats(defID))
}
