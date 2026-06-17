// Package scheduler — Phase 8 Alerting engine (server-side).
//
// Avalia regras configuráveis após cada execução terminal (OK/NOTOK, retries
// esgotados) e materializa eventos em `alert_events`. Os eventos são servidos
// via /api/alerts e empurrados em tempo real pelo hub ("alert.fired"), de modo
// que o sino/badge da UI e o toast funcionem em server mode.
//
// Espelha a semântica do engine do frontend (app/src/lib/alerting.ts): mesmas
// regras default, mesmas condições e mensagens, para que os dois modos
// (browser-only e server) se comportem igual.
package scheduler

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
)

// AlertEngine avalia regras e persiste eventos disparados.
type AlertEngine struct {
	db  *db.DB
	hub *hub.Hub

	mu        sync.Mutex
	cooldowns map[string]time.Time // ruleID -> último disparo (ephemeral)
}

func NewAlertEngine(database *db.DB, h *hub.Hub) *AlertEngine {
	return &AlertEngine{db: database, hub: h, cooldowns: map[string]time.Time{}}
}

// alertCondition espelha o discriminated-union AlertCondition do frontend.
type alertCondition struct {
	Type        string  `json:"type"`
	ThresholdMs int64   `json:"thresholdMs,omitempty"`
	MaxRetries  int     `json:"maxRetries,omitempty"`
	Rate        float64 `json:"rate,omitempty"`
	WindowSize  int     `json:"windowSize,omitempty"`
	Count       int     `json:"count,omitempty"`
}

// AlertRule — regra carregada do DB.
type AlertRule struct {
	ID              string
	Name            string
	Enabled         bool
	WorkflowPattern string
	ConditionJSON   string
	Severity        string
	Channels        string // CSV
	CooldownMs      int64
}

// AlertContext — entrada da avaliação (construída a partir da instance finalizada).
type AlertContext struct {
	WorkflowID          string
	WorkflowName        string
	Status              string // domain.StatusOK | domain.StatusNotOK
	DurationMs          int64
	MaxJobRetries       int
	RecentSuccessRate   float64
	ConsecutiveFailures int
}

// defaultRules — idênticas às do frontend (mesmos ids/cooldowns).
var defaultRules = []AlertRule{
	{ID: "rule-failure", Name: "Workflow Failure", Enabled: true, WorkflowPattern: "*",
		ConditionJSON: `{"type":"failure"}`, Severity: "critical", Channels: "toast", CooldownMs: 60000},
	{ID: "rule-slow", Name: "Slow Execution", Enabled: true, WorkflowPattern: "*",
		ConditionJSON: `{"type":"duration_exceeded","thresholdMs":30000}`, Severity: "warning", Channels: "toast", CooldownMs: 300000},
	{ID: "rule-retries", Name: "Excessive Retries", Enabled: true, WorkflowPattern: "*",
		ConditionJSON: `{"type":"retry_exceeded","maxRetries":3}`, Severity: "warning", Channels: "toast", CooldownMs: 120000},
}

// SeedDefaults insere as regras default uma vez (idempotente: pula se a tabela
// já tiver regras). Chamado no boot.
func (e *AlertEngine) SeedDefaults() {
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM alert_rules`).Scan(&n); err != nil {
		log.Printf("[alerts] seed check: %v", err)
		return
	}
	if n > 0 {
		return
	}
	for _, r := range defaultRules {
		_, err := e.db.Exec(
			`INSERT INTO alert_rules(id, name, enabled, workflow_pattern, condition_json, severity, channels, cooldown_ms)
			 VALUES(?,?,?,?,?,?,?,?)`,
			r.ID, r.Name, boolToInt(r.Enabled), r.WorkflowPattern, r.ConditionJSON, r.Severity, r.Channels, r.CooldownMs,
		)
		if err != nil {
			log.Printf("[alerts] seed rule %s: %v", r.ID, err)
		}
	}
	log.Printf("[alerts] seeded %d default rules", len(defaultRules))
}

// Evaluate roda todas as regras habilitadas contra o contexto e dispara as que
// satisfazem (respeitando cooldown). Best-effort: nunca propaga erro.
func (e *AlertEngine) Evaluate(ctx AlertContext) {
	rules, err := e.ListRules()
	if err != nil {
		return
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if !matchesWorkflow(r.WorkflowPattern, ctx.WorkflowID) {
			continue
		}
		if e.onCooldown(r.ID, r.CooldownMs) {
			continue
		}
		var cond alertCondition
		if json.Unmarshal([]byte(r.ConditionJSON), &cond) != nil {
			continue
		}
		if !evalCondition(cond, ctx) {
			continue
		}
		e.fire(r, cond, ctx)
	}
}

func (e *AlertEngine) fire(r AlertRule, cond alertCondition, ctx AlertContext) {
	msg := buildMessage(cond, ctx)
	tsMs := time.Now().UnixMilli()
	id, err := e.db.InsertID(
		`INSERT INTO alert_events(rule_id, rule_name, severity, workflow_id, workflow_name, message, acknowledged, ts_ms)
		 VALUES(?,?,?,?,?,?,0,?)`,
		r.ID, r.Name, r.Severity, ctx.WorkflowID, ctx.WorkflowName, msg, tsMs,
	)
	if err != nil {
		log.Printf("[alerts] insert event: %v", err)
		return
	}
	e.setCooldown(r.ID)
	if e.hub != nil {
		e.hub.BroadcastWeb("alert.fired", map[string]any{
			"id":           fmt.Sprintf("%d", id),
			"ruleId":       r.ID,
			"ruleName":     r.Name,
			"severity":     r.Severity,
			"timestamp":    tsMs,
			"workflowId":   ctx.WorkflowID,
			"workflowName": ctx.WorkflowName,
			"message":      msg,
			"acknowledged": false,
		})
	}
}

/* ── Cooldown (ephemeral, in-memory) ── */

func (e *AlertEngine) onCooldown(ruleID string, cooldownMs int64) bool {
	if cooldownMs <= 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	last, ok := e.cooldowns[ruleID]
	if !ok {
		return false
	}
	return time.Since(last) < time.Duration(cooldownMs)*time.Millisecond
}

func (e *AlertEngine) setCooldown(ruleID string) {
	e.mu.Lock()
	e.cooldowns[ruleID] = time.Now()
	e.mu.Unlock()
}

/* ── Condition evaluation (mirror do frontend) ── */

func matchesWorkflow(pattern, workflowID string) bool {
	return pattern == "*" || pattern == workflowID
}

func evalCondition(c alertCondition, ctx AlertContext) bool {
	switch c.Type {
	case "failure":
		return ctx.Status == string(domain.StatusNotOK)
	case "duration_exceeded":
		return ctx.DurationMs > c.ThresholdMs
	case "retry_exceeded":
		return ctx.MaxJobRetries > c.MaxRetries
	case "success_rate_below":
		return ctx.RecentSuccessRate < c.Rate
	case "consecutive_failures":
		return ctx.ConsecutiveFailures >= c.Count
	}
	return false
}

func buildMessage(c alertCondition, ctx AlertContext) string {
	wf := ctx.WorkflowName
	if wf == "" {
		wf = ctx.WorkflowID
	}
	switch c.Type {
	case "failure":
		return fmt.Sprintf("Workflow %q failed after %.1fs", wf, float64(ctx.DurationMs)/1000)
	case "duration_exceeded":
		return fmt.Sprintf("Workflow %q exceeded %ds threshold (took %.1fs)", wf, c.ThresholdMs/1000, float64(ctx.DurationMs)/1000)
	case "retry_exceeded":
		return fmt.Sprintf("Workflow %q had %d retries (limit: %d)", wf, ctx.MaxJobRetries, c.MaxRetries)
	case "success_rate_below":
		return fmt.Sprintf("Workflow %q success rate dropped to %.0f%%", wf, ctx.RecentSuccessRate*100)
	case "consecutive_failures":
		return fmt.Sprintf("Workflow %q has %d consecutive failures", wf, ctx.ConsecutiveFailures)
	}
	return fmt.Sprintf("Alert triggered for %q", wf)
}

/* ── Persistence queries (servidas pela API) ── */

// AlertEventRow — linha de evento serializável para a API.
type AlertEventRow struct {
	ID           string `json:"id"`
	RuleID       string `json:"ruleId"`
	RuleName     string `json:"ruleName"`
	Severity     string `json:"severity"`
	Timestamp    int64  `json:"timestamp"`
	WorkflowID   string `json:"workflowId"`
	WorkflowName string `json:"workflowName"`
	Message      string `json:"message"`
	Acknowledged bool   `json:"acknowledged"`
}

func (e *AlertEngine) ListEvents(limit int) ([]AlertEventRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := e.db.Query(
		`SELECT id, rule_id, rule_name, severity, workflow_id, workflow_name, message,
		        COALESCE(acknowledged,0), COALESCE(ts_ms,0)
		 FROM alert_events ORDER BY ts_ms DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertEventRow{}
	for rows.Next() {
		var ev AlertEventRow
		var idNum int64
		var ackInt int
		if err := rows.Scan(&idNum, &ev.RuleID, &ev.RuleName, &ev.Severity,
			&ev.WorkflowID, &ev.WorkflowName, &ev.Message, &ackInt, &ev.Timestamp); err != nil {
			continue
		}
		ev.ID = fmt.Sprintf("%d", idNum)
		ev.Acknowledged = ackInt == 1
		out = append(out, ev)
	}
	return out, nil
}

func (e *AlertEngine) UnacknowledgedCount() (int, error) {
	var n int
	err := e.db.QueryRow(`SELECT COUNT(*) FROM alert_events WHERE COALESCE(acknowledged,0)=0`).Scan(&n)
	return n, err
}

func (e *AlertEngine) Acknowledge(id string) error {
	_, err := e.db.Exec(`UPDATE alert_events SET acknowledged=1 WHERE id=?`, id)
	return err
}

func (e *AlertEngine) AcknowledgeAll() error {
	_, err := e.db.Exec(`UPDATE alert_events SET acknowledged=1 WHERE COALESCE(acknowledged,0)=0`)
	return err
}

// AlertRuleRow — linha de regra serializável (condition emitido como objeto).
type AlertRuleRow struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Enabled         bool            `json:"enabled"`
	WorkflowPattern string          `json:"workflowPattern"`
	Condition       json.RawMessage `json:"condition"`
	Severity        string          `json:"severity"`
	Channels        []string        `json:"channels"`
	CooldownMs      int64           `json:"cooldownMs"`
}

func (e *AlertEngine) ListRules() ([]AlertRule, error) {
	rows, err := e.db.Query(
		`SELECT id, name, COALESCE(enabled,1), workflow_pattern, condition_json, severity, channels, COALESCE(cooldown_ms,60000)
		 FROM alert_rules ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		var enInt int
		if err := rows.Scan(&r.ID, &r.Name, &enInt, &r.WorkflowPattern, &r.ConditionJSON, &r.Severity, &r.Channels, &r.CooldownMs); err != nil {
			continue
		}
		r.Enabled = enInt == 1
		out = append(out, r)
	}
	return out, nil
}

func (e *AlertEngine) ToggleRule(id string) error {
	// flip enabled — portável entre SQLite/PG (sem boolean NOT).
	var enInt int
	if err := e.db.QueryRow(`SELECT COALESCE(enabled,1) FROM alert_rules WHERE id=?`, id).Scan(&enInt); err != nil {
		return err
	}
	next := 0
	if enInt == 0 {
		next = 1
	}
	_, err := e.db.Exec(`UPDATE alert_rules SET enabled=? WHERE id=?`, next, id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
