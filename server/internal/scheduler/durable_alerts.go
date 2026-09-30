package scheduler

import (
	"database/sql"
	"encoding/json"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"strings"
	"time"
)

type durableNotification struct {
	Rule     AlertRule
	Context  AlertContext
	Message  string
	AlertID  int64
	At       int64
	Settings map[string]string
}

// Toast e destino congelado fazem parte do commit da intenção.
func durableNotificationTx(tx *db.Tx, r AlertRule, ctx AlertContext, message string, now time.Time) (string, string, error) {
	var alertID int64
	if err := tx.QueryRow("INSERT INTO alert_events(rule_id,rule_name,severity,workflow_id,workflow_name,message,acknowledged,ts_ms) VALUES(?,?,?,?,?,?,0,?) RETURNING id", r.ID, r.Name, r.Severity, ctx.WorkflowID, ctx.WorkflowName, message, now.UnixMilli()).Scan(&alertID); err != nil {
		return "", "", err
	}
	settings := map[string]string{}
	rows, err := tx.Query("SELECT key,value FROM settings WHERE key LIKE 'alert_%' OR key IN ('public_url','quickaction_secret')")
	if err != nil {
		return "", "", err
	}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			rows.Close()
			return "", "", err
		}
		settings[key] = value
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", "", err
	}
	external := false
	for _, sink := range []struct{ channel, key string }{{"slack", "alert_slack_webhook"}, {"webhook", "alert_webhook_url"}, {"email", "alert_smtp_host"}, {"pagerduty", "alert_pagerduty_routing_key"}} {
		external = external || (channelWanted(r.Channels, sink.channel) && settings[sink.key] != "")
	}
	payload, err := json.Marshal(durableNotification{r, ctx, message, alertID, now.UnixMilli(), settings})
	state := "done"
	if external {
		state = "pending"
	}
	return string(payload), state, err
}
func (s *Scheduler) durableAlerts(tx *db.Tx, a execution.Attempt, def domain.JobDefinition, id, status string, budget int) error {
	if s.alerts == nil || (status != "OK" && status != "NOTOK" && status != "RUNNING") {
		return nil
	}
	now := s.Now()
	ctx := AlertContext{WorkflowID: def.ID, WorkflowName: labelOf(def), InstanceID: id, Status: status, MaxJobRetries: budget - 1}
	if a.StartedAt != 0 {
		ctx.DurationMs = now.UnixMilli() - a.StartedAt
	}
	var avg sql.NullFloat64
	var historical int
	query := "SELECT AVG((julianday(finished_at)-julianday(started_at))*86400000),COUNT(*) FROM (SELECT started_at,finished_at FROM instance_runs WHERE definition_id=? AND status='OK' AND finished_at IS NOT NULL AND NOT(instance_id=? AND attempt=?) ORDER BY finished_at DESC LIMIT 30) recent"
	if s.db.Dialect() == db.Postgres {
		query = "SELECT AVG(EXTRACT(EPOCH FROM (finished_at-started_at))*1000),COUNT(*) FROM (SELECT started_at,finished_at FROM instance_runs WHERE definition_id=? AND status='OK' AND finished_at IS NOT NULL AND NOT(instance_id=? AND attempt=?) ORDER BY finished_at DESC LIMIT 30) recent"
	}
	if err := tx.QueryRow(query, def.ID, id, a.Attempt).Scan(&avg, &historical); err != nil {
		return err
	}
	ctx.AvgDurationMs = int64(avg.Float64)
	ctx.HistoryRuns = historical
	histories, err := tx.Query("SELECT status FROM instances WHERE definition_id=? AND status IN ('OK','NOTOK') AND finished_at IS NOT NULL ORDER BY finished_at DESC LIMIT 10", def.ID)
	if err != nil {
		return err
	}
	success, total := 0, 0
	consecutive := true
	for histories.Next() {
		var st string
		if err = histories.Scan(&st); err != nil {
			histories.Close()
			return err
		}
		total++
		if st == "OK" {
			success++
		}
		if consecutive && st == "NOTOK" {
			ctx.ConsecutiveFailures++
		} else {
			consecutive = false
		}
	}
	err = histories.Err()
	histories.Close()
	if err != nil {
		return err
	}
	if total > 0 {
		ctx.RecentSuccessRate = float64(success) / float64(total)
	}
	rows, err := tx.Query("SELECT id,name,enabled,workflow_pattern,condition_json,severity,channels,cooldown_ms FROM alert_rules ORDER BY id")
	if err != nil {
		return err
	}
	rules := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		if err = rows.Scan(&r.ID, &r.Name, &r.Enabled, &r.WorkflowPattern, &r.ConditionJSON, &r.Severity, &r.Channels, &r.CooldownMs); err != nil {
			rows.Close()
			return err
		}
		rules = append(rules, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range rules {
		var cond alertCondition
		if !r.Enabled || !matchesWorkflow(r.WorkflowPattern, def.ID) || json.Unmarshal([]byte(r.ConditionJSON), &cond) != nil || !evalCondition(cond, ctx) {
			continue
		}
		if status == "RUNNING" && cond.Type != "slow_vs_average" {
			continue
		}
		var fired int
		if err = tx.QueryRow("SELECT COUNT(*) FROM execution_effects WHERE id=?", a.ExecutionID+"-alert-"+r.ID).Scan(&fired); err != nil {
			return err
		}
		if fired != 0 {
			continue
		}
		// Lock da regra protege o cooldown durável entre instâncias/nós.
		if _, err = tx.Exec("UPDATE alert_rules SET enabled=enabled WHERE id=?", r.ID); err != nil {
			return err
		}
		var last int64
		if err = tx.QueryRow("SELECT COALESCE(MAX(ts_ms),0) FROM alert_events WHERE rule_id=? AND workflow_id=?", r.ID, def.ID).Scan(&last); err != nil {
			return err
		}
		if r.CooldownMs > 0 && now.UnixMilli()-last < r.CooldownMs {
			continue
		}
		payload, state, err := durableNotificationTx(tx, r, ctx, buildMessage(cond, ctx), now)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO execution_effects(id,execution_id,instance_id,kind,payload,state,created_at) VALUES(?,?,?,'notify',?,?,?) ON CONFLICT(id) DO NOTHING", a.ExecutionID+"-alert-"+r.ID, a.ExecutionID, id, payload, state, now.UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}
func (s *Scheduler) durableActionNotify(tx *db.Tx, def domain.JobDefinition, rule domain.ActionRule, id string) (string, string, error) {
	severity := rule.Severity
	if severity == "" {
		severity = "warning"
	}
	msg := rule.Message
	if msg == "" {
		msg = defaultActionMessage(def, rule)
	}
	r := AlertRule{ID: "rule-action", Name: "On/Do · " + labelOf(def), Severity: severity, Channels: strings.Join(rule.Channels, ",")}
	ctx := AlertContext{WorkflowID: def.ID, WorkflowName: labelOf(def), InstanceID: id}
	return durableNotificationTx(tx, r, ctx, msg, s.Now())
}
