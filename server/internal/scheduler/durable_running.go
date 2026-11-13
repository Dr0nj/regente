package scheduler

import (
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"log"
	"strconv"
	"time"
)

func (s *Scheduler) evaluateDurableRunning(now time.Time) {
	var slowRules []AlertRule
	if s.alerts != nil {
		slowRules = s.alerts.SlowRules()
	}
	// Pré-filtro somente de leitura. Sem regra vencida ou histórico para alerta,
	// não disputa os locks globais de auditoria/fila apenas para confirmar um no-op.
	// Candidatos continuam sendo revalidados integralmente na transação abaixo.
	rows, err := s.db.Query(`SELECT r.instance_id,i.definition_snapshot,COALESCE(NULLIF(i.carried_from,''),i.order_date),a.started_at,
 EXISTS(SELECT 1 FROM instance_runs h WHERE h.definition_id=i.definition_id AND h.status='OK' AND h.finished_at IS NOT NULL AND NOT(h.instance_id=i.id AND h.attempt=a.attempt))
 FROM execution_attempts a JOIN lab_orders o ON o.id=a.order_id AND o.current_execution=a.execution_id JOIN runtime_orders r ON r.order_id=o.id JOIN instances i ON i.id=r.instance_id WHERE a.state NOT IN ('succeeded','failed','cancelled') AND a.state='running' AND a.started_at>0`)
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id, raw, odate string
		var started int64
		var history bool
		if err = rows.Scan(&id, &raw, &odate, &started, &history); err != nil {
			rows.Close()
			return
		}
		var def domain.JobDefinition
		if json.Unmarshal([]byte(raw), &def) != nil || runtimeEvaluationDue(def, odate, started, now, s.sla != nil, history, slowRules) {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	for _, id := range ids {
		var before, after int
		changed := false
		if err = s.durable.RuntimeUpdate(id, func(tx *db.Tx, o execution.Order, a execution.Attempt) error {
			if err := s.guardLeadership(tx); err != nil {
				return err
			}
			if err := tx.QueryRow("SELECT COUNT(*) FROM execution_effects WHERE instance_id=?", id).Scan(&before); err != nil {
				return err
			}
			defer func() {
				if tx.QueryRow("SELECT COUNT(*) FROM execution_effects WHERE instance_id=?", id).Scan(&after) == nil {
					changed = after > before
				}
			}()
			var raw, expected, odate, status string
			var budget, blocked int
			if err := tx.QueryRow("SELECT i.definition_snapshot,COALESCE(l.snapshot_checksum,''),COALESCE(NULLIF(i.carried_from,''),i.order_date),i.status,COALESCE(i.attempts,1) FROM instances i LEFT JOIN daily_order_ledger l ON l.instance_id=i.id WHERE i.id=?", id).Scan(&raw, &expected, &odate, &status, &budget); err != nil {
				return err
			}
			var source string
			if err := tx.QueryRow("SELECT snapshot_checksum FROM lab_orders WHERE id=?", o.ID).Scan(&source); err != nil {
				return err
			}
			var def domain.JobDefinition
			if status != "RUNNING" || json.Unmarshal([]byte(raw), &def) != nil || def.BusinessTime == nil || def.BusinessTime.Validate() != nil || digest(raw) != source || (expected != "" && expected != source) {
				return execution.ErrConflict
			}
			if err := tx.QueryRow("SELECT COUNT(*) FROM daily_order_ledger l JOIN daily_runs d ON d.order_date=l.order_date WHERE l.instance_id=? AND d.state NOT IN ('completed','legacy')", id).Scan(&blocked); err != nil {
				return err
			}
			if blocked != 0 {
				return execution.ErrConflict
			}
			ev := actionEvent{kind: "runtime", runMin: int((now.UnixMilli() - a.StartedAt) / 60000)}
			for i, rule := range def.Actions {
				if !actionMatches(rule, ev) {
					continue
				}
				effectID := a.ExecutionID + "-runtime-action-" + strconv.Itoa(i)
				var exists int
				if err := tx.QueryRow("SELECT COUNT(*) FROM execution_effects WHERE id=?", effectID).Scan(&exists); err != nil {
					return err
				}
				if exists > 0 {
					continue
				}
				payload, err := json.Marshal(durableEffectPayload{Definition: def, Rule: rule, OrderDate: odate})
				if err != nil {
					return err
				}
				state := "done"
				switch rule.Do {
				case "set-condition":
					err = durableCondition(tx, rule.Condition, odate, "actions", false, now)
				case "notify":
					var text string
					text, state, err = s.durableActionNotify(tx, def, rule, id)
					payload = []byte(text)
				case "run-job":
					state = "pending"
				case "set-ok":
				default:
					return execution.ErrInvalid
				}
				if err != nil {
					return err
				}
				if _, err = tx.Exec("INSERT INTO execution_effects(id,execution_id,instance_id,kind,payload,state,created_at) VALUES(?,?,?,?,?,?,?)", effectID, a.ExecutionID, id, rule.Do, string(payload), state, now.UnixMilli()); err != nil {
					return err
				}
				if err = durableEvent(tx, id, "action", "actions", effectID, now); err != nil {
					return err
				}
			}
			if err := s.durableAlerts(tx, a, def, id, "RUNNING", budget); err != nil {
				return err
			}
			if s.sla != nil && def.SLA != nil {
				if def.SLA.ExpectedDurationMin > 0 && ev.runMin >= def.SLA.ExpectedDurationMin && now.UnixMilli()-a.StartedAt > int64(def.SLA.ExpectedDurationMin)*60000 {
					if err := s.durableSLATx(tx, a, def, id, "duration", fmt.Sprintf("running beyond %dmin", def.SLA.ExpectedDurationMin), now); err != nil {
						return err
					}
				}
				if c, ok := frozenCalendar(def); ok && def.SLA.DeadlineHM != "" {
					deadline := c.At(odate, def.SLA.DeadlineHM)
					if !deadline.IsZero() && now.After(deadline) {
						return s.durableSLATx(tx, a, def, id, "deadline", "not finished by deadline "+def.SLA.DeadlineHM, now)
					}
				}
			}
			return nil
		}); err != nil && err != execution.ErrConflict {
			log.Printf("[execution] runtime evaluation retained: %v", err)
		}
		if err == nil && changed {
			s.hub.BroadcastWeb("instance.changed", map[string]any{"id": id, "effectsChanged": true})
			s.hub.BroadcastWeb("conditions.changed", map[string]string{"by": id})
			s.hub.BroadcastWeb("alerts.changed", map[string]string{"by": id})
			s.hub.BroadcastWeb("sla.breach", map[string]string{"instance": id})
		}
	}
}

func runtimeEvaluationDue(def domain.JobDefinition, odate string, started int64, now time.Time, sla, history bool, slowRules []AlertRule) bool {
	ev := actionEvent{kind: "runtime", runMin: int((now.UnixMilli() - started) / 60000)}
	for _, rule := range def.Actions {
		if actionMatches(rule, ev) {
			return true
		}
	}
	if sla && def.SLA != nil {
		if def.SLA.ExpectedDurationMin > 0 && now.UnixMilli()-started > int64(def.SLA.ExpectedDurationMin)*60000 {
			return true
		}
		if c, ok := frozenCalendar(def); ok && def.SLA.DeadlineHM != "" {
			deadline := c.At(odate, def.SLA.DeadlineHM)
			if !deadline.IsZero() && now.After(deadline) {
				return true
			}
		}
	}
	if history {
		for _, rule := range slowRules {
			if matchesWorkflow(rule.WorkflowPattern, def.ID) {
				return true
			}
		}
	}
	return false
}

type durableWebhook struct {
	URL  string
	Body map[string]any
}

func (s *Scheduler) durableSLATx(tx *db.Tx, a execution.Attempt, def domain.JobDefinition, id, kind, message string, now time.Time) error {
	var exists int
	if err := tx.QueryRow("SELECT COUNT(*) FROM sla_breaches WHERE instance_id=? AND kind=?", id, kind).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return nil
	}
	severity := def.SLA.Severity
	if severity == "" {
		severity = "warning"
	}
	state := "done"
	if def.SLA.WebhookURL != "" {
		state = "pending"
	}
	if _, err := tx.Exec("INSERT INTO sla_breaches(instance_id,definition_id,kind,severity,message,detected_at,notified) VALUES(?,?,?,?,?,?,?)", id, def.ID, kind, severity, message, now, 0); err != nil {
		return err
	}
	payload, err := json.Marshal(durableWebhook{URL: def.SLA.WebhookURL, Body: map[string]any{"event": "sla.breach", "instance": id, "definition": def.ID, "kind": kind, "severity": severity, "message": message, "timestamp": now.UTC().Format(time.RFC3339)}})
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO execution_effects(id,execution_id,instance_id,kind,payload,state,created_at) VALUES(?,?,?,'webhook',?,?,?)", a.ExecutionID+"-sla-"+kind, a.ExecutionID, id, string(payload), state, now.UnixMilli())
	return err
}
