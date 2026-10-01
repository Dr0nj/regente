package execution

import (
	"database/sql"
	"errors"
)

// Resultado + terminalidade + evento + ACK da outbox são uma transação.
// I10: efeitos de negócio locais e suas intenções fazem parte do mesmo commit.
func (e *Engine) Complete(r Result) (Receipt, error) {
	if len(r.Output) > MaxOutputBytes {
		return Receipt{}, ErrOutputLimit
	}
	tx, err := e.begin()
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	o, a, err := lockedAttempt(tx, r.ExecutionID)
	if err != nil {
		return Receipt{}, err
	}
	if !identityOK(a, r.Identity) {
		return Receipt{}, ErrConflict
	}
	sum := resultChecksum(r)
	if a.resultHash != "" {
		if sum != a.resultHash {
			return Receipt{}, ErrConflict
		}
		return receipt(a.ExecutionID, true), nil
	}
	if !current(o, a) || a.AcceptedAt == 0 || a.StartedAt == 0 || terminal(a.State) {
		return Receipt{}, ErrConflict
	}
	state := "succeeded"
	if r.ExitCode != 0 {
		state = "failed"
	}
	now := e.now()
	res, err := tx.Exec(`UPDATE execution_attempts SET state=?,exit_code=?,result_output=?,result_checksum=?,finished_at=?,lease_until=0 WHERE execution_id=? AND fence=? AND agent_id=? AND state=? AND result_checksum=''`, state, r.ExitCode, r.Output, sum, now, a.ExecutionID, a.Fence, a.AgentID, a.State)
	if err != nil {
		return Receipt{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Receipt{}, ErrConflict
	}
	res, err = tx.Exec(`UPDATE lab_orders SET state=? WHERE id=? AND current_execution=? AND fence=?`, state, o.ID, a.ExecutionID, a.Fence)
	if err != nil {
		return Receipt{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Receipt{}, ErrConflict
	}
	if _, err = tx.Exec(`UPDATE execution_outbox SET state='acked',lease_until=0 WHERE execution_id=?`, a.ExecutionID); err != nil {
		return Receipt{}, err
	}
	a.State = state
	a.FinishedAt = now
	a.LastContact = now
	if err = e.transition(tx, o, a.Attempt, "result", &r); err != nil {
		return Receipt{}, err
	}
	if err = event(tx, a.ExecutionID, "result_recorded", now); err != nil {
		return Receipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return Receipt{}, err
	}
	e.notify(a.ExecutionID)
	return receipt(a.ExecutionID, false), nil
}

func (e *Engine) Append(r Output) (Receipt, error) {
	if r.Seq < 1 || r.Chunk == "" {
		return Receipt{}, ErrInvalid
	}
	if len(r.Chunk) > MaxOutputBytes {
		return Receipt{}, ErrOutputLimit
	}
	tx, err := e.begin()
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	o, a, err := lockedAttempt(tx, r.ExecutionID)
	if err != nil {
		return Receipt{}, err
	}
	if !identityOK(a, r.Identity) {
		return Receipt{}, ErrConflict
	}
	sum := checksum(r.Chunk)
	var stored string
	err = tx.QueryRow(`SELECT checksum FROM execution_output WHERE execution_id=? AND seq=?`, a.ExecutionID, r.Seq).Scan(&stored)
	if err == nil {
		if stored != sum {
			return Receipt{}, ErrConflict
		}
		return receipt(a.ExecutionID, true), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	if !current(o, a) || a.StartedAt == 0 || terminal(a.State) || r.Seq != a.lastSeq+1 {
		return Receipt{}, ErrConflict
	}
	if a.outputBytes+int64(len(r.Chunk)) > MaxOutputBytes {
		return Receipt{}, ErrOutputLimit
	}
	if _, err = tx.Exec(`INSERT INTO execution_output(execution_id,seq,chunk,checksum,created_at) VALUES(?,?,?,?,?)`, a.ExecutionID, r.Seq, r.Chunk, sum, e.now()); err != nil {
		return Receipt{}, err
	}
	res, err := tx.Exec(`UPDATE execution_attempts SET output_bytes=output_bytes+?,last_output_seq=? WHERE execution_id=? AND state=? AND last_output_seq=?`, len(r.Chunk), r.Seq, a.ExecutionID, a.State, a.lastSeq)
	if err != nil {
		return Receipt{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Receipt{}, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return Receipt{}, err
	}
	return receipt(a.ExecutionID, false), nil
}

type Chunk struct {
	Seq   int64  `json:"seq"`
	Chunk string `json:"chunk"`
}

func (e *Engine) Output(id string, after int64) ([]Chunk, error) {
	if after < 0 {
		return nil, ErrInvalid
	}
	if _, err := e.Attempt(id); err != nil {
		return nil, err
	}
	rows, err := e.DB.Query(`SELECT seq,chunk FROM execution_output WHERE execution_id=? AND seq>? ORDER BY seq LIMIT 100`, id, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Chunk{}
	for rows.Next() {
		var c Chunk
		if err = rows.Scan(&c.Seq, &c.Chunk); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

type Metrics struct {
	Pending, Leased, Paused, Uncertain int
	Deliveries                         int64
}

func (e *Engine) Metrics() (Metrics, error) {
	var m Metrics
	err := e.DB.QueryRow(`SELECT COALESCE(SUM(CASE WHEN state='pending' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN state='leased' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN state='paused' THEN 1 ELSE 0 END),0),COALESCE(SUM(deliveries),0) FROM execution_outbox`).Scan(&m.Pending, &m.Leased, &m.Paused, &m.Deliveries)
	if err != nil {
		return m, err
	}
	err = e.DB.QueryRow(`SELECT COUNT(*) FROM execution_attempts WHERE state='uncertain'`).Scan(&m.Uncertain)
	return m, err
}
