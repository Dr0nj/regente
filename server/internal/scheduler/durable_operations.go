package scheduler

import (
	"encoding/json"
	"errors"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
)

// Todas as operações compartilham o lock de admissão com Start/Complete.
func (s *Scheduler) DurableAction(actor, id, action string) (string, error) {
	if s.durable == nil {
		return "", execution.ErrInvalid
	}
	if action == "cancel" {
		o, err := s.durable.RuntimeOrder(id)
		if err != nil && !errors.Is(err, execution.ErrNotFound) {
			return "", err
		}
		if err == nil && o.CurrentExecution != "" {
			a, err := s.durable.Attempt(o.CurrentExecution)
			if err != nil {
				return "", err
			}
			if a.State != "succeeded" && a.State != "failed" && a.State != "cancelled" {
				if _, err = s.durable.CancelFor(o.ID, actor); err != nil {
					return "", err
				}
				var state string
				err = s.db.QueryRow("SELECT status FROM instances WHERE id=?", id).Scan(&state)
				return state, err
			}
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE execution_queue_lock SET marker=marker WHERE id=1"); err != nil {
		return "", err
	}
	// O lock da instância impede a reivindicação por caminhos concorrentes legados.
	res, err := tx.Exec("UPDATE instances SET status=status WHERE id=?", id)
	if err != nil {
		return "", err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", execution.ErrNotFound
	}
	var status, raw, expected, odate, scope, held string
	if err = tx.QueryRow("SELECT i.status,COALESCE(i.definition_snapshot,''),COALESCE(l.snapshot_checksum,''),COALESCE(NULLIF(i.carried_from,''),i.order_date),COALESCE(i.hold_scope,''),COALESCE(i.held_from_status,'') FROM instances i LEFT JOIN daily_order_ledger l ON l.instance_id=i.id WHERE i.id=?", id).Scan(&status, &raw, &expected, &odate, &scope, &held); err != nil {
		return "", err
	}
	var unresolved int
	if err = tx.QueryRow("SELECT COUNT(*) FROM runtime_orders r JOIN execution_attempts a ON a.order_id=r.order_id WHERE r.instance_id=? AND a.state NOT IN ('succeeded','failed','cancelled')", id).Scan(&unresolved); err != nil {
		return "", err
	}
	if unresolved != 0 || status == "RUNNING" || status == "UNCERTAIN" {
		return "", execution.ErrConflict
	}
	var def domain.JobDefinition
	if action == "set-ok" || action == "rerun" || action == "force" {
		if json.Unmarshal([]byte(raw), &def) != nil || def.BusinessTime == nil || def.BusinessTime.Validate() != nil || (expected != "" && expected != digest(raw)) {
			return "", execution.ErrConflict
		}
		var blocked int
		if err = tx.QueryRow("SELECT COUNT(*) FROM daily_order_ledger l JOIN daily_runs d ON d.order_date=l.order_date WHERE l.instance_id=? AND d.state NOT IN ('completed','legacy')", id).Scan(&blocked); err != nil {
			return "", err
		}
		if blocked != 0 {
			return "", execution.ErrConflict
		}
	}
	next := status
	switch action {
	case "hold":
		if status == "HELD" {
			return status, nil
		}
		next = "HELD"
		_, err = tx.Exec("UPDATE instances SET held_from_status=status,status='HELD',hold_scope='' WHERE id=?", id)
	case "release":
		if status != "HELD" || scope != "" {
			return "", execution.ErrConflict
		}
		next = held
		if next == "" {
			next = "WAITING"
		}
		_, err = tx.Exec("UPDATE instances SET status=?,held_from_status='',hold_scope='' WHERE id=?", next, id)
	case "cancel":
		if status != "WAITING" && status != "HELD" {
			return "", execution.ErrConflict
		}
		next = "CANCELLED"
		_, err = tx.Exec("UPDATE instances SET status='CANCELLED',held_from_status='',finished_at=? WHERE id=?", s.Now(), id)
	case "rerun":
		if status != "OK" && status != "NOTOK" && status != "CANCELLED" {
			return "", execution.ErrConflict
		}
		next = "WAITING"
		_, err = tx.Exec("UPDATE instances SET status='WAITING',started_at=NULL,finished_at=NULL,exit_code=NULL,output=NULL,held_from_status='',attempts=1,forced=CASE WHEN COALESCE(force_mode,'')='' THEN 0 ELSE forced END WHERE id=?", id)
	case "force":
		if status != "WAITING" && status != "HELD" {
			return "", execution.ErrConflict
		}
		next = "WAITING"
		_, err = tx.Exec("UPDATE instances SET status='WAITING',forced=1,force_mode='',held_from_status='',scheduled_at=? WHERE id=?", s.Now(), id)
	case "set-ok":
		if status != "WAITING" && status != "NOTOK" && status != "CANCELLED" {
			return "", execution.ErrConflict
		}
		next = "OK"
		_, err = tx.Exec("UPDATE instances SET status='OK',exit_code=0,finished_at=COALESCE(finished_at,?) WHERE id=?", s.Now(), id)
		if err == nil {
			err = durableOut(tx, def, odate, actor, s.Now())
		}
	case "confirm":
		if status != "WAITING" && status != "HELD" {
			return "", execution.ErrConflict
		}
		_, err = tx.Exec("UPDATE instances SET confirmed=1 WHERE id=?", id)
	case "delete":
		if status != "HELD" {
			return "", execution.ErrConflict
		}
		var pending int
		if err = tx.QueryRow("SELECT COUNT(*) FROM execution_effects WHERE instance_id=? AND state NOT IN ('done','cancelled')", id).Scan(&pending); err != nil {
			return "", err
		}
		if pending != 0 {
			return "", execution.ErrConflict
		}
		for _, table := range []string{"instance_events", "instance_output", "execution_resource_holds"} {
			if _, err = tx.Exec("DELETE FROM "+table+" WHERE instance_id=?", id); err != nil {
				return "", err
			}
		}
		if _, err = tx.Exec("DELETE FROM instances WHERE id=?", id); err != nil {
			return "", err
		}
		next = "deleted"
	default:
		return "", execution.ErrInvalid
	}
	if err != nil {
		return "", err
	}
	if action != "delete" {
		if err = durableEvent(tx, id, action, actor, "durable operator action", s.Now()); err != nil {
			return "", err
		}
	}
	if action == "cancel" || action == "set-ok" || action == "rerun" {
		if _, err = tx.Exec("DELETE FROM execution_resource_holds WHERE instance_id=?", id); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	if s.resources != nil && (action == "cancel" || action == "set-ok" || action == "rerun" || action == "delete") {
		s.resources.Release(id)
	}
	s.hub.BroadcastWeb("instance.changed", map[string]any{"id": id, "status": next})
	return next, nil
}
