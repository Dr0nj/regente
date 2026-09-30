package execution

import (
	"database/sql"
	"errors"
	"strings"
)

func (e *Engine) Resolve(exec string, fence int64, key, actor, decision, reason string, stopped bool) (Attempt, error) {
	reason = strings.TrimSpace(reason)
	if !validKey(key) || actor == "" || reason == "" || len(reason) > 2000 || !stopped || (decision != "succeeded" && decision != "failed" && decision != "cancelled") {
		return Attempt{}, ErrInvalid
	}
	tx, err := e.begin()
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	o, a, err := lockedAttempt(tx, exec)
	if err != nil {
		return Attempt{}, err
	}
	var priorExec, priorActor, priorDecision, priorReason string
	err = tx.QueryRow("SELECT execution_id,actor,decision,reason FROM execution_decisions WHERE request_key=?", key).Scan(&priorExec, &priorActor, &priorDecision, &priorReason)
	if err == nil {
		if priorExec != exec || priorActor != actor || priorDecision != decision || priorReason != reason || a.Fence != fence {
			return Attempt{}, ErrConflict
		}
		return a.Attempt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, err
	}
	if !current(o, a) || a.Fence != fence || (a.State != "uncertain" && a.State != "cancel_requested") {
		return Attempt{}, ErrConflict
	}
	now := e.now()
	code := 0
	if decision == "failed" {
		code = 1
	}
	if decision == "cancelled" {
		code = -1
	}
	r := Result{Identity: Identity{ProtocolVersion, a.ExecutionID, a.Fence, a.AgentID}, ExitCode: code, Output: "[operator resolution] " + reason}
	if _, err = tx.Exec("UPDATE execution_attempts SET state=?,finished_at=?,lease_until=0,resolved_at=?,resolved_by=?,reason=?,exit_code=?,result_output=?,result_checksum=? WHERE execution_id=?", decision, now, now, actor, reason, code, r.Output, "operator:"+resultChecksum(r), exec); err != nil {
		return Attempt{}, err
	}
	if _, err = tx.Exec("UPDATE lab_orders SET state=? WHERE id=?", decision, o.ID); err != nil {
		return Attempt{}, err
	}
	if _, err = tx.Exec("UPDATE execution_outbox SET state='acked',lease_until=0 WHERE execution_id=?", exec); err != nil {
		return Attempt{}, err
	}
	if _, err = tx.Exec("INSERT INTO execution_decisions(request_key,execution_id,actor,decision,reason,created_at) VALUES(?,?,?,?,?,?)", key, exec, actor, decision, reason, now); err != nil {
		return Attempt{}, err
	}
	a.State = decision
	a.ResolvedAt = now
	a.ResolvedBy = actor
	a.Reason = reason
	if err = e.transition(tx, o, a.Attempt, "resolved", &r); err != nil {
		return Attempt{}, err
	}
	if err = event(tx, exec, "operator_resolved", now); err != nil {
		return Attempt{}, err
	}
	if err = tx.Commit(); err != nil {
		return Attempt{}, err
	}
	e.notify(exec)
	return e.Attempt(exec)
}
