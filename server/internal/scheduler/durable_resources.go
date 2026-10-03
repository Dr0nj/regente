package scheduler

import (
	"database/sql"
	"encoding/json"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
)

// Reconcilia a fonte durável, sem apagar reservas incertas ou órfãs. Um erro
// impede admissão; reconstrução não inventa uma execução a partir de RUNNING.
func (s *Scheduler) rebuildDurableResources() (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE execution_queue_lock SET marker=marker WHERE id=1"); err != nil {
		return 0, err
	}
	if err = s.guardLeadership(tx); err != nil {
		return 0, err
	}
	rows, err := tx.Query("SELECT r.instance_id,o.current_execution,o.snapshot,o.snapshot_checksum FROM runtime_orders r JOIN lab_orders o ON o.id=r.order_id JOIN execution_attempts a ON a.execution_id=o.current_execution JOIN instances i ON i.id=r.instance_id WHERE a.state NOT IN ('succeeded','failed','cancelled') OR (i.status='WAITING' AND COALESCE(i.attempts,1)>1)")
	if err != nil {
		return 0, err
	}
	type record struct{ id, exec, raw, sum string }
	records := []record{}
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.id, &r.exec, &r.raw, &r.sum); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, r := range records {
		var def domain.JobDefinition
		if digest(r.raw) != r.sum || json.Unmarshal([]byte(r.raw), &def) != nil {
			return 0, execution.ErrConflict
		}
		for name, qty := range def.Resources {
			if qty <= 0 {
				continue
			}
			if _, err = tx.Exec("INSERT INTO resources(name,capacity) VALUES(?,1) ON CONFLICT(name) DO NOTHING", name); err != nil {
				return 0, err
			}
			if _, err = tx.Exec("INSERT INTO execution_resource_holds(instance_id,name,quantity,execution_id) VALUES(?,?,?,?) ON CONFLICT(instance_id,name) DO NOTHING", r.id, name, qty, r.exec); err != nil {
				return 0, err
			}
			var held int
			var owner string
			if err = tx.QueryRow("SELECT quantity,execution_id FROM execution_resource_holds WHERE instance_id=? AND name=?", r.id, name).Scan(&held, &owner); err != nil {
				return 0, err
			}
			if held != qty || owner != r.exec {
				return 0, execution.ErrConflict
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(records), nil
}

func (s *Scheduler) durableShortfalls(id string, want map[string]int) ([]ResourceShortfall, error) {
	result := []ResourceShortfall{}
	for name, qty := range want {
		if qty <= 0 {
			continue
		}
		capacity := 1
		var c int
		err := s.db.QueryRow("SELECT capacity FROM resources WHERE name=?", name).Scan(&c)
		if err == nil {
			capacity = c
		} else if err != sql.ErrNoRows {
			return nil, err
		}
		var used, held int
		if err = s.db.QueryRow("SELECT COALESCE(SUM(quantity),0),COALESCE(SUM(CASE WHEN instance_id=? THEN quantity ELSE 0 END),0) FROM execution_resource_holds WHERE name=?", id, name).Scan(&used, &held); err != nil {
			return nil, err
		}
		if used-held+qty > capacity {
			result = append(result, ResourceShortfall{Name: name, Want: qty, Used: used, Capacity: capacity})
		}
	}
	return result, nil
}

// O banco é a fonte autoritativa entre nós, inclusive para efeitos incertos.
func (s *Scheduler) DurableResourceSnapshot() ([]ResourceState, error) {
	rows, err := s.db.Query("SELECT r.name,r.capacity,COALESCE(SUM(h.quantity),0) FROM resources r LEFT JOIN execution_resource_holds h ON h.name=r.name GROUP BY r.name,r.capacity ORDER BY r.name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ResourceState{}
	for rows.Next() {
		var state ResourceState
		if err = rows.Scan(&state.Name, &state.Capacity, &state.Used); err != nil {
			return nil, err
		}
		out = append(out, state)
	}
	return out, rows.Err()
}
func (s *Scheduler) DurableResourceChange(name string, capacity int, remove bool) error {
	return s.DurableResourceChangeUsing(s.db, name, capacity, remove)
}
func (s *Scheduler) DurableResourceChangeUsing(database *db.DB, name string, capacity int, remove bool) error {
	if name == "" || capacity < 0 {
		return execution.ErrInvalid
	}
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE execution_queue_lock SET marker=marker WHERE id=1"); err != nil {
		return err
	}
	if remove {
		var used int
		if err = tx.QueryRow("SELECT COALESCE(SUM(quantity),0) FROM execution_resource_holds WHERE name=?", name).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return execution.ErrConflict
		}
		res, err := tx.Exec("DELETE FROM resources WHERE name=?", name)
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
	} else {
		if _, err = tx.Exec("INSERT INTO resources(name,capacity) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET capacity=excluded.capacity", name, capacity); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.resources != nil {
		s.resources.LoadFromDB(s.db)
	}
	return nil
}
