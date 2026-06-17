// Package api — Phase 8 Alerting endpoints.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/go-chi/chi/v5"
)

// GET /api/alerts?limit=200 — eventos disparados (mais recentes primeiro).
func (s *server) listAlertEvents(w http.ResponseWriter, r *http.Request) {
	eng := s.cfg.Scheduler.Alerts()
	if eng == nil {
		writeJSON(w, 200, []scheduler.AlertEventRow{})
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	out, err := eng.ListEvents(limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, out)
}

// POST /api/alerts/{id}/ack — reconhece um evento.
func (s *server) ackAlertEvent(w http.ResponseWriter, r *http.Request) {
	eng := s.cfg.Scheduler.Alerts()
	if eng == nil {
		http.Error(w, "alerting not configured", 503)
		return
	}
	if err := eng.Acknowledge(chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/alerts/ack-all — reconhece todos os eventos pendentes.
func (s *server) ackAllAlertEvents(w http.ResponseWriter, r *http.Request) {
	eng := s.cfg.Scheduler.Alerts()
	if eng == nil {
		http.Error(w, "alerting not configured", 503)
		return
	}
	if err := eng.AcknowledgeAll(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// GET /api/alerts/rules — regras configuradas (condition como objeto).
func (s *server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	eng := s.cfg.Scheduler.Alerts()
	if eng == nil {
		writeJSON(w, 200, []scheduler.AlertRuleRow{})
		return
	}
	rules, err := eng.ListRules()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	out := make([]scheduler.AlertRuleRow, 0, len(rules))
	for _, r := range rules {
		channels := []string{}
		for _, c := range strings.Split(r.Channels, ",") {
			if t := strings.TrimSpace(c); t != "" {
				channels = append(channels, t)
			}
		}
		out = append(out, scheduler.AlertRuleRow{
			ID:              r.ID,
			Name:            r.Name,
			Enabled:         r.Enabled,
			WorkflowPattern: r.WorkflowPattern,
			Condition:       json.RawMessage(r.ConditionJSON),
			Severity:        r.Severity,
			Channels:        channels,
			CooldownMs:      r.CooldownMs,
		})
	}
	writeJSON(w, 200, out)
}

// POST /api/alerts/rules/{id}/toggle — habilita/desabilita uma regra.
func (s *server) toggleAlertRule(w http.ResponseWriter, r *http.Request) {
	eng := s.cfg.Scheduler.Alerts()
	if eng == nil {
		http.Error(w, "alerting not configured", 503)
		return
	}
	if err := eng.ToggleRule(chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
