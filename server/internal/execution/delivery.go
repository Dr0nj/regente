package execution

import (
	"encoding/json"
)

// Claim só entrega mensagens atribuídas ao agente. O commit antecede qualquer envio.
// Redelivery mantém executionId, fencing e chave; timeout do claim não prova aceite.
func (e *Engine) Claim(agent string) (*Envelope, error) { return e.ClaimCapacity(agent, true) }
func (e *Engine) ClaimCapacity(agent string, available bool) (*Envelope, error) {
	if agent == "" {
		return nil, ErrInvalid
	}
	now := e.now()
	// Os candidatos são apenas pistas; todos são revalidados sob o lock da ordem.
	rows, err := e.DB.Query(`SELECT b.id FROM execution_outbox b JOIN execution_attempts a ON a.execution_id=b.execution_id WHERE a.agent_id=? AND b.state IN ('pending','leased') AND b.next_at<=? AND b.lease_until<=? AND (?=1 OR b.kind='cancel') ORDER BY CASE WHEN b.kind='cancel' THEN 0 ELSE 1 END,b.created_at,b.id LIMIT 32`, agent, now, now, func() int {
		if available {
			return 1
		}
		return 0
	}())
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		msg, err := e.claimOne(id, agent, now)
		if err != nil {
			return nil, err
		}
		if msg != nil {
			return msg, nil
		}
	}
	return nil, nil
}
func (e *Engine) claimOne(id, agent string, now int64) (*Envelope, error) {
	tx, err := e.begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exec string
	if err = tx.QueryRow(`SELECT execution_id FROM execution_outbox WHERE id=?`, id).Scan(&exec); err != nil {
		return nil, err
	}
	o, a, err := lockedAttempt(tx, exec)
	if err != nil {
		return nil, err
	}
	var state, kind, raw string
	var deliveries int
	var lease, next int64
	if err = tx.QueryRow(`SELECT state,kind,payload,deliveries,lease_until,next_at FROM execution_outbox WHERE id=?`, id).Scan(&state, &kind, &raw, &deliveries, &lease, &next); err != nil {
		return nil, err
	}
	if (state != "pending" && state != "leased") || lease > now || next > now || a.AgentID != agent || !current(o, a) {
		return nil, nil
	}
	if terminal(a.State) || (a.State == "uncertain" && kind == "dispatch") || (kind == "dispatch" && a.State != "dispatch_pending" && a.State != "dispatching") {
		return nil, nil
	}
	if deliveries >= e.MaxDeliveries {
		if _, err = tx.Exec(`UPDATE execution_outbox SET state='paused' WHERE id=?`, id); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`UPDATE execution_attempts SET state='uncertain',reason='delivery limit exhausted; acceptance unknown' WHERE execution_id=?`, exec); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`UPDATE lab_orders SET state='uncertain' WHERE id=?`, o.ID); err != nil {
			return nil, err
		}
		a.State = "uncertain"
		a.Reason = "delivery limit exhausted; acceptance unknown"
		if err = e.transition(tx, o, a.Attempt, "uncertain", nil); err != nil {
			return nil, err
		}
		if err = event(tx, exec, "delivery_uncertain", now); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	var msg Envelope
	if json.Unmarshal([]byte(raw), &msg) != nil || msg.Protocol != ProtocolVersion || msg.ExecutionID != a.ExecutionID || msg.AgentID != agent || msg.Fence != a.Fence || msg.Kind != kind {
		return nil, ErrConflict
	}
	until := now + e.DeliveryLease.Milliseconds()
	// Retry tem espera persistida e limitada; com lease default, o limite superior é 5min.
	delay := min(int64(1000)<<min(deliveries, 8), int64(300000))
	if _, err = tx.Exec(`UPDATE execution_outbox SET state='leased',deliveries=deliveries+1,lease_until=?,next_at=? WHERE id=?`, until, max(until, now+delay), id); err != nil {
		return nil, err
	}
	if kind == "dispatch" {
		if _, err = tx.Exec(`UPDATE execution_attempts SET state='dispatching' WHERE execution_id=?`, exec); err != nil {
			return nil, err
		}
	}
	if err = event(tx, exec, "delivered_"+kind, now); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (e *Engine) Acknowledge(id Identity, kind string) (Receipt, error) {
	return e.AcknowledgeReason(id, kind, "")
}
func (e *Engine) AcknowledgeReason(id Identity, kind, reason string) (Receipt, error) {
	if len(reason) > 2000 {
		return Receipt{}, ErrInvalid
	}
	if kind != "accepted" && kind != "started" && kind != "heartbeat" && kind != "cancelled" && kind != "uncertain" && kind != "status" {
		return Receipt{}, ErrInvalid
	}
	tx, err := e.begin()
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	o, a, err := lockedAttempt(tx, id.ExecutionID)
	if err != nil {
		return Receipt{}, err
	}
	if !identityOK(a, id) {
		return Receipt{}, ErrConflict
	}
	if kind == "status" {
		return phaseReceipt(o, a, true), nil
	}
	if (kind == "cancelled" && a.State == "cancelled") || (kind == "accepted" && a.AcceptedAt != 0) || (kind == "started" && a.StartedAt != 0) {
		return phaseReceipt(o, a, true), nil
	}
	if kind == "uncertain" && terminal(a.State) {
		return phaseReceipt(o, a, true), nil
	}
	if !current(o, a) || terminal(a.State) {
		return Receipt{}, ErrConflict
	}
	now := e.now()
	next := a.State
	dup := false
	switch kind {
	case "uncertain":
		next = "uncertain"
		a.Reason = reason
		if a.Reason == "" {
			a.Reason = "agent reported an unresolved effect"
		}
		if _, err = tx.Exec(`UPDATE lab_orders SET state='uncertain' WHERE id=?`, o.ID); err != nil {
			return Receipt{}, err
		}
		if _, err = tx.Exec(`UPDATE execution_outbox SET state='paused',lease_until=0 WHERE execution_id=? AND state IN ('pending','leased')`, a.ExecutionID); err != nil {
			return Receipt{}, err
		}

	case "accepted":
		if a.AcceptedAt != 0 {
			return phaseReceipt(o, a, true), nil
		}
		if a.State != "dispatching" && a.State != "uncertain" && a.State != "cancel_requested" {
			return Receipt{}, ErrConflict
		}
		if a.State != "cancel_requested" {
			next = "accepted"
		}
		if _, err = tx.Exec(`UPDATE execution_attempts SET accepted_at=? WHERE execution_id=?`, now, a.ExecutionID); err != nil {
			return Receipt{}, err
		}
		if _, err = tx.Exec(`UPDATE execution_outbox SET state='acked',lease_until=0 WHERE execution_id=? AND kind='dispatch'`, a.ExecutionID); err != nil {
			return Receipt{}, err
		}
	case "started":
		if a.StartedAt != 0 {
			return phaseReceipt(o, a, true), nil
		}
		if a.AcceptedAt == 0 || a.State == "dispatch_pending" || a.State == "dispatching" {
			return Receipt{}, ErrConflict
		}
		if a.State != "cancel_requested" && a.State != "uncertain" {
			next = "running"
		}
		if _, err = tx.Exec(`UPDATE execution_attempts SET started_at=? WHERE execution_id=?`, now, a.ExecutionID); err != nil {
			return Receipt{}, err
		}
	case "heartbeat":
		if a.AcceptedAt == 0 {
			return Receipt{}, ErrConflict
		}
	case "cancelled":
		if a.State != "cancel_requested" && a.State != "uncertain" {
			return Receipt{}, ErrConflict
		}
		var n int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM execution_outbox WHERE execution_id=? AND kind='cancel'`, a.ExecutionID).Scan(&n); err != nil {
			return Receipt{}, err
		}
		if n != 1 {
			return Receipt{}, ErrConflict
		}
		next = "cancelled"
		if _, err = tx.Exec(`UPDATE execution_attempts SET finished_at=? WHERE execution_id=?`, now, a.ExecutionID); err != nil {
			return Receipt{}, err
		}
		if _, err = tx.Exec(`UPDATE execution_outbox SET state='acked',lease_until=0 WHERE execution_id=?`, a.ExecutionID); err != nil {
			return Receipt{}, err
		}
		if _, err = tx.Exec(`UPDATE lab_orders SET state='cancelled' WHERE id=? AND current_execution=? AND fence=?`, o.ID, a.ExecutionID, a.Fence); err != nil {
			return Receipt{}, err
		}
	}
	leaseUntil := now + e.ExecutionLease.Milliseconds()
	if terminal(next) || next == "uncertain" {
		leaseUntil = 0
	}
	if _, err = tx.Exec(`UPDATE execution_attempts SET state=?,lease_until=?,last_contact=?,reason=? WHERE execution_id=? AND state=?`, next, leaseUntil, now, a.Reason, a.ExecutionID, a.State); err != nil {
		return Receipt{}, err
	}
	a.State = next
	a.LastContact = now
	if kind == "started" {
		a.StartedAt = now
	}
	if kind == "accepted" {
		a.AcceptedAt = now
	}
	if err = e.transition(tx, o, a.Attempt, kind, nil); err != nil {
		return Receipt{}, err
	}
	if kind != "heartbeat" {
		if err = event(tx, a.ExecutionID, kind, now); err != nil {
			return Receipt{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Receipt{}, err
	}
	e.notify(a.ExecutionID)
	a.State = next
	if kind == "started" {
		a.StartedAt = now
	}
	return phaseReceipt(o, a, dup), nil
}

// Cancel antes de qualquer entrega é definitivo. Após entrega, espera ACK/resultado.
func (e *Engine) Cancel(orderID string) (Attempt, error) { return e.CancelFor(orderID, "operator") }
func (e *Engine) CancelFor(orderID, actor string) (Attempt, error) {
	if actor == "" {
		return Attempt{}, ErrInvalid
	}
	tx, err := e.begin()
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	o, err := lockOrder(tx, orderID)
	if err != nil {
		return Attempt{}, err
	}
	if o.CurrentExecution == "" {
		return Attempt{}, ErrConflict
	}
	a, err := scanAttempt(tx.QueryRow(attemptSelect, o.CurrentExecution))
	if err != nil {
		return Attempt{}, err
	}
	if a.State == "cancel_requested" || a.State == "cancelled" {
		return a.Attempt, nil
	}
	if terminal(a.State) {
		return Attempt{}, ErrConflict
	}
	var deliveries int
	if err = tx.QueryRow(`SELECT deliveries FROM execution_outbox WHERE execution_id=? AND kind='dispatch'`, a.ExecutionID).Scan(&deliveries); err != nil {
		return Attempt{}, err
	}
	now := e.now()
	state, orderState := "cancel_requested", "uncertain"
	if deliveries == 0 && a.State == "dispatch_pending" {
		state, orderState = "cancelled", "cancelled"
		if _, err = tx.Exec(`UPDATE execution_outbox SET state='cancelled' WHERE execution_id=?`, a.ExecutionID); err != nil {
			return Attempt{}, err
		}
	} else {
		msg, err := json.Marshal(Envelope{ProtocolVersion, "cancel", a.ExecutionID + "-cancel", a.ExecutionID, o.ID, a.Attempt.Attempt, a.AgentID, a.Fence, a.ExecutionID, nil})
		if err != nil {
			return Attempt{}, err
		}
		if _, err = tx.Exec(`INSERT INTO execution_outbox(id,execution_id,kind,state,payload,created_at) VALUES(?,?,'cancel','pending',?,?) ON CONFLICT(execution_id,kind) DO NOTHING`, a.ExecutionID+"-cancel", a.ExecutionID, string(msg), now); err != nil {
			return Attempt{}, err
		}
		if _, err = tx.Exec(`UPDATE execution_outbox SET state='cancelled' WHERE execution_id=? AND kind='dispatch' AND state!='acked'`, a.ExecutionID); err != nil {
			return Attempt{}, err
		}
	}
	if _, err = tx.Exec(`UPDATE execution_attempts SET state=?,finished_at=? WHERE execution_id=? AND state=?`, state, func() int64 {
		if state == "cancelled" {
			return now
		}
		return 0
	}(), a.ExecutionID, a.State); err != nil {
		return Attempt{}, err
	}
	if _, err = tx.Exec(`UPDATE lab_orders SET state=? WHERE id=?`, orderState, o.ID); err != nil {
		return Attempt{}, err
	}
	a.State = state
	if err = e.transition(tx, o, a.Attempt, state, nil); err != nil {
		return Attempt{}, err
	}
	if _, err = tx.Exec("INSERT INTO execution_decisions(request_key,execution_id,actor,decision,reason,created_at) VALUES(?,?,?,'cancel_requested','operator requested cancellation',?) ON CONFLICT(request_key) DO NOTHING", "cancel:"+a.ExecutionID, a.ExecutionID, actor, now); err != nil {
		return Attempt{}, err
	}
	if err = event(tx, a.ExecutionID, "cancel_requested", now); err != nil {
		return Attempt{}, err
	}
	if err = tx.Commit(); err != nil {
		return Attempt{}, err
	}
	e.notify(a.ExecutionID)
	return e.Attempt(a.ExecutionID)
}

func (e *Engine) Reconcile() (int, error) {
	now := e.now()
	rows, err := e.DB.Query(`SELECT execution_id FROM execution_attempts WHERE state IN ('accepted','running','cancel_requested') AND lease_until>0 AND lease_until<=?`, now)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, id := range ids {
		ok, err := e.expire(id, now)
		if err != nil {
			return changed, err
		}
		if ok {
			changed++
		}
	}
	return changed, nil
}
func (e *Engine) expire(id string, now int64) (bool, error) {
	tx, err := e.begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	o, a, err := lockedAttempt(tx, id)
	if err != nil {
		return false, err
	}
	if !current(o, a) || terminal(a.State) || a.State == "uncertain" || a.LeaseUntil == 0 || a.LeaseUntil > now {
		return false, nil
	}
	if _, err = tx.Exec(`UPDATE execution_attempts SET state='uncertain',reason='execution lease expired; effect outcome unknown' WHERE execution_id=? AND state=?`, id, a.State); err != nil {
		return false, err
	}
	if _, err = tx.Exec(`UPDATE lab_orders SET state='uncertain' WHERE id=? AND current_execution=? AND fence=?`, o.ID, id, a.Fence); err != nil {
		return false, err
	}
	a.State = "uncertain"
	a.Reason = "execution lease expired; effect outcome unknown"
	if err = e.transition(tx, o, a.Attempt, "uncertain", nil); err != nil {
		return false, err
	}
	if err = event(tx, id, "lease_uncertain", now); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	e.notify(id)
	return true, nil
}
