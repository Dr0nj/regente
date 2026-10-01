package scheduler

import (
	"database/sql"
	"errors"
	"github.com/Dr0nj/regente-server/internal/execution"
	"strings"
)

func (s *Scheduler) ResolveEffect(id, key, actor, decision, reason, classification string, stopped, risk bool, generation int) error {
	reason = strings.TrimSpace(reason)
	if key == "" || len(key) > 128 || actor == "" || reason == "" || len(reason) > 2000 || !stopped || generation < 0 {
		return execution.ErrInvalid
	}
	if decision != "done" && decision != "cancelled" && decision != "retry" {
		return execution.ErrInvalid
	}
	if decision == "retry" && (!risk || (classification != "idempotent" && classification != "verified-absent" && classification != "duplicate-risk-accepted")) {
		return execution.ErrInvalid
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec("UPDATE execution_effects SET state=state WHERE id=?", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return execution.ErrNotFound
	}
	var priorGeneration int
	var priorID, priorActor, priorDecision, priorReason, priorClass string
	err = tx.QueryRow("SELECT effect_id,actor,decision,reason,classification,generation FROM execution_effect_decisions WHERE request_key=?", key).Scan(&priorID, &priorActor, &priorDecision, &priorReason, &priorClass, &priorGeneration)
	if err == nil {
		if priorID != id || priorActor != actor || priorDecision != decision || priorReason != reason || priorClass != classification || priorGeneration != generation {
			return execution.ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var currentGeneration int
	var state, instance string
	if err = tx.QueryRow("SELECT state,instance_id,generation FROM execution_effects WHERE id=?", id).Scan(&state, &instance, &currentGeneration); err != nil {
		return err
	}
	if currentGeneration != generation || (state != "uncertain" && state != "pending") {
		return execution.ErrConflict
	}
	next := decision
	resolved := s.Now().UnixMilli()
	if next == "retry" {
		next = "pending"
		resolved = 0
	}
	if _, err = tx.Exec("UPDATE execution_effects SET state=?,reason=?,resolved_by=?,resolved_at=?,lease_until=0,generation=generation+1 WHERE id=?", next, reason, actor, resolved, id); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO execution_effect_decisions(request_key,effect_id,actor,decision,reason,classification,generation,created_at) VALUES(?,?,?,?,?,?,?,?)", key, id, actor, decision, reason, classification, generation, s.Now().UnixMilli()); err != nil {
		return err
	}
	if err = durableEvent(tx, instance, "effect-resolved", actor, id+" "+decision+" "+classification+": "+reason, s.Now()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.hub.BroadcastWeb("instance.changed", map[string]any{"id": instance, "effectsChanged": true})
	return nil
}
