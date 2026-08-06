// Package api — handlers do Bloco 2 (Control-M parity).
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/scheduler"
)

// O decode do {name} da rota vive em urlName() (urlname.go) — compartilhado por
// todos os handlers que endereçam um recurso por nome (calendars, resources,
// variables, conditions, folders, templates), não só por conditions.

// === F14 — Calendars ===

func (s *server) listCalendars(w http.ResponseWriter, r *http.Request) {
	cs := s.cfg.Scheduler.Calendars()
	if cs == nil {
		writeJSON(w, 200, []domain.Calendar{})
		return
	}
	out, err := cs.List()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) getCalendar(w http.ResponseWriter, r *http.Request) {
	cs := s.cfg.Scheduler.Calendars()
	if cs == nil {
		http.Error(w, "calendars not configured", http.StatusServiceUnavailable)
		return
	}
	name := urlName(r, "name")
	cal, err := cs.Get(name)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, 200, cal)
}

func (s *server) saveCalendar(w http.ResponseWriter, r *http.Request) {
	cs := s.cfg.Scheduler.Calendars()
	if cs == nil {
		http.Error(w, "calendars not configured", http.StatusServiceUnavailable)
		return
	}
	name := urlName(r, "name")
	var cal domain.Calendar
	if err := json.NewDecoder(r.Body).Decode(&cal); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	cal.Name = name
	if err := cs.Save(cal); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, cal)
}

func (s *server) deleteCalendar(w http.ResponseWriter, r *http.Request) {
	cs := s.cfg.Scheduler.Calendars()
	if cs == nil {
		http.Error(w, "calendars not configured", http.StatusServiceUnavailable)
		return
	}
	if err := cs.Delete(urlName(r, "name")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}

// === F15 — Resources ===

func (s *server) listResources(w http.ResponseWriter, r *http.Request) {
	rt := s.cfg.Scheduler.Resources()
	if rt == nil {
		writeJSON(w, 200, []scheduler.ResourceState{})
		return
	}
	writeJSON(w, 200, rt.Snapshot())
}

func (s *server) setResourceCapacity(w http.ResponseWriter, r *http.Request) {
	rt := s.cfg.Scheduler.Resources()
	if rt == nil {
		http.Error(w, "resources not configured", http.StatusServiceUnavailable)
		return
	}
	name := urlName(r, "name")
	var body struct {
		Capacity int `json:"capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	rt.SetCapacity(name, body.Capacity)
	writeJSON(w, 200, map[string]any{"name": name, "capacity": body.Capacity})
}

// deleteResource — DELETE /api/resources/{name}. Recusa se used > 0.
func (s *server) deleteResource(w http.ResponseWriter, r *http.Request) {
	rt := s.cfg.Scheduler.Resources()
	if rt == nil {
		http.Error(w, "resources not configured", http.StatusServiceUnavailable)
		return
	}
	name := urlName(r, "name")
	if err := rt.Delete(name); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(204)
}

// === F16 — Conditions ===

func (s *server) listConditions(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Scheduler.Conditions()
	if c == nil {
		writeJSON(w, 200, []domain.Condition{})
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "*"
	}
	out, err := c.List(scope)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) setCondition(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Scheduler.Conditions()
	if c == nil {
		http.Error(w, "conditions not configured", http.StatusServiceUnavailable)
		return
	}
	name := urlName(r, "name")
	scope := r.URL.Query().Get("scope")
	u, _ := auth.FromContext(r.Context())
	actor := "operator"
	if u != nil {
		actor = u.Username
	}
	if err := c.Set(name, scope, actor); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if s.cfg.Hub != nil {
		s.cfg.Hub.BroadcastWeb("condition.set", map[string]any{"name": name, "scope": scope, "by": actor})
	}
	// BUG-10: job que recebe uma condição roda NA HORA — cutuca o tick em vez
	// de deixar o WAIT_CONDITION esperando o próximo ciclo.
	go s.cfg.Scheduler.Tick()
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *server) unsetCondition(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Scheduler.Conditions()
	if c == nil {
		http.Error(w, "conditions not configured", http.StatusServiceUnavailable)
		return
	}
	name := urlName(r, "name")
	scope := r.URL.Query().Get("scope")
	if err := c.Unset(name, scope); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if s.cfg.Hub != nil {
		s.cfg.Hub.BroadcastWeb("condition.unset", map[string]any{"name": name, "scope": scope})
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// === F19 — SLA ===

func (s *server) listSLABreaches(w http.ResponseWriter, r *http.Request) {
	sl := s.cfg.Scheduler.SLA()
	if sl == nil {
		writeJSON(w, 200, []domain.SLABreach{})
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	out, err := sl.ListBreaches(limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, out)
}

// === F21 — Forecast ===

func (s *server) getForecast(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = s.cfg.Scheduler.TodayDate() // DAY-1: a diária corrente (vira no daily_at)
	}
	defs := s.cfg.Scheduler.Defs()
	cals := map[string]*domain.Calendar{}
	if cs := s.cfg.Scheduler.Calendars(); cs != nil {
		list, _ := cs.List()
		for i := range list {
			cals[list[i].Name] = &list[i]
		}
	}
	report := scheduler.Forecast(defs, cals, date)
	writeJSON(w, 200, report)
}

// getForecastRange — Forecast de N dias à frente (Control-M "≥1 semana"). Mesmo
// gating do RunDaily por dia (via IsScheduledOn). `from` default = hoje; `days`
// default = 7, clamp [1,366]. Devolve um ForecastReport por dia.
func (s *server) getForecastRange(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	if from == "" {
		from = time.Now().Format("2006-01-02")
	}
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	defs := s.cfg.Scheduler.Defs()
	cals := map[string]*domain.Calendar{}
	if cs := s.cfg.Scheduler.Calendars(); cs != nil {
		list, _ := cs.List()
		for i := range list {
			cals[list[i].Name] = &list[i]
		}
	}
	writeJSON(w, 200, scheduler.ForecastRange(defs, cals, from, days))
}

// === F22 — Analytics ===

func (s *server) analyticsSummary(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since") // YYYY-MM-DD
	if since == "" {
		since = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	rows, err := s.cfg.DB.Query(
		`SELECT status, COUNT(*) FROM instances WHERE order_date >= ? GROUP BY status`,
		since,
	)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		_ = rows.Scan(&st, &n)
		counts[st] = n
	}
	if !rowsOK(w, rows) {
		return
	}
	writeJSON(w, 200, map[string]any{"since": since, "counts": counts})
}

func (s *server) analyticsTopFailing(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	if since == "" {
		since = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}
	rows, err := s.cfg.DB.Query(
		`SELECT definition_id, COUNT(*) AS fails FROM instances
		 WHERE order_date >= ? AND status='NOTOK'
		 GROUP BY definition_id ORDER BY fails DESC LIMIT 20`,
		since,
	)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	type item struct {
		DefID string `json:"defId"`
		Fails int    `json:"fails"`
	}
	out := []item{}
	for rows.Next() {
		var i item
		_ = rows.Scan(&i.DefID, &i.Fails)
		out = append(out, i)
	}
	if !rowsOK(w, rows) {
		return
	}
	writeJSON(w, 200, map[string]any{"since": since, "topFailing": out})
}

func (s *server) analyticsMTTR(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	if since == "" {
		since = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}
	// MTTR: avg(finished_at - started_at) em segundos para OK. ST-1 — mede sobre
	// EXECUÇÕES (instance_runs): o par de timestamps da instance é mutado por Set
	// OK/retry/cyclic e inflava a média (ver scheduler/runs.go).
	rows, err := s.cfg.DB.Query(
		`SELECT definition_id,
		        AVG(CAST((julianday(finished_at) - julianday(started_at)) * 86400 AS INTEGER)) AS avg_sec,
		        COUNT(*) AS runs
		 FROM instance_runs
		 WHERE order_date >= ? AND status='OK' AND finished_at IS NOT NULL
		 GROUP BY definition_id ORDER BY avg_sec DESC LIMIT 20`,
		since,
	)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	type item struct {
		DefID  string  `json:"defId"`
		AvgSec float64 `json:"avgSec"`
		Runs   int     `json:"runs"`
	}
	out := []item{}
	for rows.Next() {
		var i item
		_ = rows.Scan(&i.DefID, &i.AvgSec, &i.Runs)
		out = append(out, i)
	}
	if !rowsOK(w, rows) {
		return
	}
	writeJSON(w, 200, map[string]any{"since": since, "mttr": out})
}
