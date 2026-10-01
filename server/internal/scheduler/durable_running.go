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
	rows, err := s.db.Query("SELECT r.instance_id FROM runtime_orders r JOIN lab_orders o ON o.id=r.order_id JOIN execution_attempts a ON a.execution_id=o.current_execution WHERE a.state='running' AND a.started_at>0")
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return
		}
		ids = append(ids, id)
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
