package execution

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"slices"
	"strings"
)

type record struct {
	Attempt
	resultHash           string
	outputBytes, lastSeq int64
}

const attemptSelect = `SELECT execution_id,order_id,attempt,fence,agent_id,state,lease_until,accepted_at,started_at,finished_at,result_checksum,output_bytes,last_output_seq,last_contact,reason,resolved_at,resolved_by FROM execution_attempts WHERE execution_id=?`

func scanAttempt(row *sql.Row) (record, error) {
	var a record
	err := row.Scan(&a.ExecutionID, &a.OrderID, &a.Attempt.Attempt, &a.Fence, &a.AgentID, &a.State, &a.LeaseUntil, &a.AcceptedAt, &a.StartedAt, &a.FinishedAt, &a.resultHash, &a.outputBytes, &a.lastSeq, &a.LastContact, &a.Reason, &a.ResolvedAt, &a.ResolvedBy)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return a, err
}
func (e *Engine) Attempt(id string) (Attempt, error) {
	a, err := scanAttempt(e.DB.QueryRow(attemptSelect, id))
	return a.Attempt, err
}
func scanOrder(row *sql.Row) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.SourceInstanceID, &o.State, &o.CurrentExecution, &o.Attempt, &o.Fence, &o.Runtime)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return o, err
}

const orderSelect = `SELECT id,source_instance_id,state,current_execution,attempt_number,fence,runtime FROM lab_orders WHERE id=?`

func (e *Engine) Order(id string) (Order, error) { return scanOrder(e.DB.QueryRow(orderSelect, id)) }
func lockOrder(tx *db.Tx, id string) (Order, error) {
	if _, err := tx.Exec(`UPDATE lab_orders SET fence=fence WHERE id=?`, id); err != nil {
		return Order{}, err
	}
	return scanOrder(tx.QueryRow(orderSelect, id))
}
func lockedAttempt(tx *db.Tx, id string) (Order, record, error) {
	var order string
	if err := tx.QueryRow(`SELECT order_id FROM execution_attempts WHERE execution_id=?`, id).Scan(&order); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return Order{}, record{}, err
	}
	o, err := lockOrder(tx, order)
	if err != nil {
		return o, record{}, err
	}
	a, err := scanAttempt(tx.QueryRow(attemptSelect, id))
	return o, a, err
}
func identityOK(a record, id Identity) bool {
	return id.Protocol == ProtocolVersion && id.ExecutionID == a.ExecutionID && id.Fence == a.Fence && id.AgentID == a.AgentID
}
func current(o Order, a record) bool {
	return o.CurrentExecution == a.ExecutionID && o.Fence == a.Fence
}
func event(tx *db.Tx, id, kind string, now int64) error {
	_, err := tx.Exec(`INSERT INTO execution_events(execution_id,kind,created_at) VALUES(?,?,?) ON CONFLICT(execution_id,kind) DO NOTHING`, id, kind, now)
	return err
}

// Copia um snapshot verificado, sem transferir a ordem original ao laboratório.
func (e *Engine) CreateOrder(source, key string) (Order, error) {
	if !validKey(key) || source == "" {
		return Order{}, ErrInvalid
	}
	var previousID, previousSource string
	previousErr := e.DB.QueryRow(`SELECT id,source_instance_id FROM lab_orders WHERE request_key=?`, key).Scan(&previousID, &previousSource)
	if previousErr == nil {
		if previousSource != source {
			return Order{}, ErrConflict
		}
		return e.Order(previousID)
	}
	if !errors.Is(previousErr, sql.ErrNoRows) {
		return Order{}, previousErr
	}
	var raw, expected, defID string
	if err := e.DB.QueryRow(`SELECT i.definition_id,COALESCE(i.definition_snapshot,''),COALESCE(l.snapshot_checksum,'') FROM instances i LEFT JOIN daily_order_ledger l ON l.instance_id=i.id WHERE i.id=?`, source).Scan(&defID, &raw, &expected); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return Order{}, err
	}
	var def domain.JobDefinition
	if raw == "" || json.Unmarshal([]byte(raw), &def) != nil || def.ID == "" || def.JobType == "" || def.ID != defID || def.BusinessTime == nil || def.BusinessTime.Validate() != nil || (expected != "" && expected != checksum(raw)) {
		return Order{}, ErrInvalid
	}
	// Daily parcial não é fonte operacional válida nem no laboratório.
	var state string
	err := e.DB.QueryRow(`SELECT d.state FROM daily_order_ledger l JOIN daily_runs d ON d.order_date=l.order_date WHERE l.instance_id=?`, source).Scan(&state)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Order{}, err
	}
	if err == nil && state != "completed" && state != "legacy" {
		return Order{}, ErrConflict
	}
	id, err := newID()
	if err != nil {
		return Order{}, err
	}
	_, err = e.DB.Exec(`INSERT INTO lab_orders(id,source_instance_id,request_key,snapshot,snapshot_checksum,environment,state,created_at) VALUES(?,?,?,?,?,?,'ready',?) ON CONFLICT(request_key) DO NOTHING`, id, source, key, raw, checksum(raw), def.Environment, e.now())
	if err != nil {
		return Order{}, err
	}
	var storedSource, storedSum string
	err = e.DB.QueryRow(`SELECT id,source_instance_id,snapshot_checksum FROM lab_orders WHERE request_key=?`, key).Scan(&id, &storedSource, &storedSum)
	if err != nil {
		return Order{}, err
	}
	if storedSource != source || storedSum != checksum(raw) {
		return Order{}, ErrConflict
	}
	return e.Order(id)
}

func (e *Engine) Start(orderID, agent, key, intent string) (Attempt, error) {
	if !validKey(key) || agent == "" || (intent != "start" && intent != "retry" && intent != "rerun") {
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
	var oldID, oldAgent, oldIntent string
	err = tx.QueryRow(`SELECT execution_id,agent_id,intent FROM execution_attempts WHERE order_id=? AND request_key=?`, orderID, key).Scan(&oldID, &oldAgent, &oldIntent)
	if err == nil {
		if oldAgent != agent || oldIntent != intent {
			return Attempt{}, ErrConflict
		}
		a, err := scanAttempt(tx.QueryRow(attemptSelect, oldID))
		return a.Attempt, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, err
	}
	if (o.CurrentExecution == "" && intent != "start") || (o.CurrentExecution != "" && (!terminal(o.State) || intent == "start")) || (intent == "retry" && o.State != "failed") {
		return Attempt{}, ErrConflict
	}
	// Um lock global leve torna o teto de fila exato também com várias ordens/nós.
	if _, err = tx.Exec(`UPDATE execution_queue_lock SET marker=marker WHERE id=1`); err != nil {
		return Attempt{}, err
	}
	var pending int
	if err = tx.QueryRow(`SELECT (SELECT COUNT(*) FROM execution_attempts WHERE state NOT IN ('succeeded','failed','cancelled'))+(SELECT COUNT(*) FROM execution_effects WHERE state NOT IN ('done','cancelled'))`).Scan(&pending); err != nil {
		return Attempt{}, err
	}
	if pending >= e.MaxPending {
		return Attempt{}, ErrCapacity
	}
	var raw, sum, env, caps string
	if err = tx.QueryRow(`SELECT snapshot,snapshot_checksum,environment FROM lab_orders WHERE id=?`, orderID).Scan(&raw, &sum, &env); err != nil {
		return Attempt{}, err
	}
	var def domain.JobDefinition
	if checksum(raw) != sum || json.Unmarshal([]byte(raw), &def) != nil {
		return Attempt{}, ErrInvalid
	}
	var machineEnv string
	var internal bool
	if err = tx.QueryRow(`SELECT environment,capabilities,internal FROM machine_principals WHERE agent_id=?`, agent).Scan(&machineEnv, &caps, &internal); err != nil {
		return Attempt{}, ErrInvalid
	}
	if env != machineEnv || !slices.Contains(strings.Split(caps, ","), Capability) || !slices.Contains(strings.Split(caps, ","), strings.ToUpper(def.JobType)) || (def.AgentID != "" && def.AgentID != agent && !(o.Runtime && internal && def.AgentID == "SERVER-AGENT")) {
		return Attempt{}, ErrConflict
	}
	if o.Runtime && e.Prepare != nil {
		def, err = e.Prepare(tx, o, def)
		if err != nil {
			return Attempt{}, err
		}
	}
	id, err := newID()
	if err != nil {
		return Attempt{}, err
	}
	number, fence := o.Attempt+1, o.Fence+1
	now := e.now()
	payload, err := json.Marshal(Envelope{ProtocolVersion, "dispatch", id + "-dispatch", id, orderID, number, agent, fence, id, &def})
	if err != nil {
		return Attempt{}, err
	}
	if _, err = tx.Exec(`INSERT INTO execution_attempts(execution_id,order_id,attempt,fence,agent_id,request_key,intent,state,created_at) VALUES(?,?,?,?,?,?,?,'dispatch_pending',?)`, id, orderID, number, fence, agent, key, intent, now); err != nil {
		return Attempt{}, err
	}
	if _, err = tx.Exec(`INSERT INTO execution_outbox(id,execution_id,kind,state,payload,created_at) VALUES(?,?,'dispatch','pending',?,?)`, id+"-dispatch", id, string(payload), now); err != nil {
		return Attempt{}, err
	}
	if _, err = tx.Exec(`UPDATE lab_orders SET current_execution=?,attempt_number=?,fence=?,state='active' WHERE id=?`, id, number, fence, orderID); err != nil {
		return Attempt{}, err
	}
	o.CurrentExecution = id
	o.Attempt = number
	o.Fence = fence
	o.State = "active"
	if err = e.transition(tx, o, Attempt{ExecutionID: id, OrderID: orderID, Attempt: number, Fence: fence, AgentID: agent, State: "dispatch_pending"}, "planned", nil); err != nil {
		return Attempt{}, err
	}
	if err = event(tx, id, "planned", now); err != nil {
		return Attempt{}, err
	}
	if err = tx.Commit(); err != nil {
		return Attempt{}, err
	}
	e.notify(id)
	return e.Attempt(id)
}
