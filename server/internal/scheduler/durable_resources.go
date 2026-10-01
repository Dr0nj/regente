package scheduler

import (
	"database/sql"
	"github.com/Dr0nj/regente-server/internal/execution"
)

func (s *Scheduler) rebuildDurableResources() (int, error) {
	rows, err := s.db.Query("SELECT instance_id,name,quantity FROM execution_resource_holds")
	if err != nil {
		return 0, err
	}
	held := map[string]map[string]int{}
	for rows.Next() {
		var id, name string
		var qty int
		if err = rows.Scan(&id, &name, &qty); err != nil {
			rows.Close()
			return 0, err
		}
		if held[id] == nil {
			held[id] = map[string]int{}
		}
		held[id][name] = qty
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for id, want := range held {
		s.resources.Reacquire(id, want)
	}
	return len(held), nil
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
	if name == "" || capacity < 0 {
		return execution.ErrInvalid
	}
	tx, err := s.db.Begin()
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
	s.resources.LoadFromDB(s.db)
	return nil
}
