package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"log"
	"time"
)

func (s *Scheduler) drainDurableEffects() {
	if s.durable == nil {
		return
	}
	if err := s.leaderExec("UPDATE execution_effects SET state='uncertain',reason='worker disappeared after claiming this effect; external outcome unknown',lease_until=0 WHERE state='claimed' AND lease_until>0 AND lease_until<=?", s.Now().UnixMilli()); err != nil {
		log.Printf("[effects] recovery: %v", err)
		return
	}
	rows, err := s.db.Query("SELECT id FROM execution_effects WHERE state='pending' ORDER BY created_at,id LIMIT 100")
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
		if s.stopping() {
			return
		}
		if err = s.durableEffect(id); err != nil {
			log.Printf("[effects] %s retained: %v", id, err)
		}
	}
}
func (s *Scheduler) durableEffect(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.guardLeadership(tx); err != nil {
		return err
	}
	// Writer/row lock precede reads; dois nós não podem despachar a mesma intenção.
	res, err := tx.Exec("UPDATE execution_effects SET state=state WHERE id=? AND state='pending'", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return nil
	}
	var kind, raw, instance string
	var generation int
	if err = tx.QueryRow("SELECT kind,payload,instance_id,generation FROM execution_effects WHERE id=?", id).Scan(&kind, &raw, &instance, &generation); err != nil {
		return err
	}
	if kind == "run-job" {
		var p durableEffectPayload
		if json.Unmarshal([]byte(raw), &p) != nil {
			return errors.New("invalid frozen action")
		}
		if err = s.durableForceTx(tx, id, instance, p.Rule.TargetJob); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE execution_effects SET state='done' WHERE id=?", id); err != nil {
			return err
		}
		return tx.Commit()
	}
	if kind != "notify" && kind != "terminal-hooks" && kind != "webhook" {
		return fmt.Errorf("unsupported effect kind %s", kind)
	}
	if _, err = tx.Exec("UPDATE execution_effects SET state='claimed',lease_until=?,generation=generation+1 WHERE id=?", s.Now().Add(time.Minute).UnixMilli(), id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	generation++
	outcome := "done"
	reason := ""
	switch kind {
	case "webhook":
		var p durableWebhook
		if err = json.Unmarshal([]byte(raw), &p); err == nil {
			err = postJSON(p.URL, p.Body, id)
		}
	case "notify":
		var p durableNotification
		if err = json.Unmarshal([]byte(raw), &p); err == nil {
			err = durableDeliver(p, id)
		}
	case "terminal-hooks":
		if s.variables != nil {
			for _, m := range setVarDirective.FindAllStringSubmatch(raw, maxSetVarsPerJob) {
				if _, err = s.variables.Set(m[1], m[2], "job:"+instance); err != nil {
					break
				}
			}
		}
	}
	if err != nil {
		outcome = "uncertain"
		reason = "effect receipt unavailable; verify external state before resolving or repeating"
	}
	receiptTx, storageErr := s.db.Begin()
	if storageErr != nil {
		return storageErr
	}
	defer receiptTx.Rollback()
	res, storageErr = receiptTx.Exec("UPDATE execution_effects SET state=?,reason=?,lease_until=0 WHERE id=? AND state IN ('claimed','uncertain') AND resolved_at=0 AND generation=?", outcome, reason, id, generation)
	if storageErr != nil {
		return storageErr
	}
	n, storageErr = res.RowsAffected()
	if storageErr != nil {
		return storageErr
	}
	if n == 1 && kind == "webhook" && outcome == "done" {
		var p durableWebhook
		if json.Unmarshal([]byte(raw), &p) == nil {
			if _, storageErr = receiptTx.Exec("UPDATE sla_breaches SET notified=1 WHERE instance_id=? AND kind=?", instance, p.Body["kind"]); storageErr != nil {
				return storageErr
			}
		}
	}
	if storageErr = receiptTx.Commit(); storageErr != nil {
		return storageErr
	}
	s.hub.BroadcastWeb("instance.changed", map[string]any{"id": instance, "effectsChanged": true})
	if kind == "notify" {
		s.hub.BroadcastWeb("alerts.changed", map[string]string{"by": instance})
	}
	if kind == "terminal-hooks" {
		s.hub.BroadcastWeb("variables.changed", map[string]string{"by": instance})
	}
	return err
}
func (s *Scheduler) durableForceTx(tx *db.Tx, effectID, source, target string) error {
	var def domain.JobDefinition
	found := false
	for _, candidate := range s.Defs() {
		if candidate.ID == target {
			def = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("target definition %s is unavailable; intent remains pending", target)
	}
	calendar := s.BusinessCalendar()
	if err := calendar.Validate(); err != nil {
		return err
	}
	def = s.freezeTime(def, calendar)
	now := s.Now()
	date := calendar.BusinessDate(now)
	id := target + "-ACTION-" + effectID
	snap, err := json.Marshal(def)
	if err != nil {
		return err
	}
	mc := frozenMonitorCols(def)
	var exists int
	if err = tx.QueryRow("SELECT COUNT(*) FROM instances WHERE id=?", id).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return errors.New("action instance already exists without a confirmed effect")
	}
	if _, err = tx.Exec("INSERT INTO instances(id,definition_id,team,order_date,status,scheduled_at,forced,force_mode,definition_snapshot,dry_run,label,job_type,confirm_req,environment,pinned_agent,conds_in,conds_out_add,resources,cond_logic) VALUES(?,?,?,?,?,?,1,?,?,?,?,?,?,?,?,?,?,?,?)", id, target, def.Team, date, "WAITING", now, ForceModeOrder, string(snap), boolToInt(def.DryRun), mc.label, mc.jobType, mc.confirmReq, mc.environment, mc.pinned, mc.condsIn, mc.condsOutAdd, mc.resources, mc.condLogic); err != nil {
		return err
	}
	return durableEvent(tx, id, "force-ordered", "actions", "source="+source+" effect="+effectID, now)
}
