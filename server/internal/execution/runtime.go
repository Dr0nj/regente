package execution

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Dr0nj/regente-server/internal/domain"
)

func (e *Engine) RuntimeOrder(instance string) (Order, error) {
	var id string
	err := e.DB.QueryRow("SELECT order_id FROM runtime_orders WHERE instance_id=?", instance).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	return e.Order(id)
}

// O snapshot real não é reinterpretado como uma tentativa legada. Apenas WAITING pode entrar.
func (e *Engine) CreateRuntimeOrder(instance string) (Order, error) {
	if o, err := e.RuntimeOrder(instance); err == nil {
		return o, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Order{}, err
	}
	tx, err := e.begin()
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback()
	var raw, expected, defID, status string
	if err = tx.QueryRow("SELECT i.definition_id,i.status,i.definition_snapshot,COALESCE(l.snapshot_checksum,'') FROM instances i LEFT JOIN daily_order_ledger l ON l.instance_id=i.id WHERE i.id=?", instance).Scan(&defID, &status, &raw, &expected); err != nil {
		return Order{}, err
	}
	var def domain.JobDefinition
	if status != "WAITING" || json.Unmarshal([]byte(raw), &def) != nil || def.ID != defID || def.BusinessTime == nil || def.BusinessTime.Validate() != nil || (expected != "" && checksum(raw) != expected) {
		return Order{}, ErrConflict
	}
	var state string
	err = tx.QueryRow("SELECT d.state FROM daily_order_ledger l JOIN daily_runs d ON d.order_date=l.order_date WHERE l.instance_id=?", instance).Scan(&state)
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
	if _, err = tx.Exec("INSERT INTO lab_orders(id,source_instance_id,request_key,snapshot,snapshot_checksum,environment,state,created_at,runtime) VALUES(?,?,?,?,?,?,'ready',?,1) ON CONFLICT(request_key) DO NOTHING", id, instance, "runtime:"+instance, raw, checksum(raw), def.Environment, e.now()); err != nil {
		return Order{}, err
	}
	if err = tx.QueryRow("SELECT id FROM lab_orders WHERE request_key=? AND runtime=1", "runtime:"+instance).Scan(&id); err != nil {
		return Order{}, err
	}
	if _, err = tx.Exec("INSERT INTO runtime_orders(instance_id,order_id) VALUES(?,?) ON CONFLICT(instance_id) DO NOTHING", instance, id); err != nil {
		return Order{}, err
	}
	if err = tx.Commit(); err != nil {
		return Order{}, err
	}
	return e.RuntimeOrder(instance)
}
