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
// activeResourceOrdersQuery — ordens cujas reservas precisam existir: tentativa
// corrente ainda viva OU instance aguardando retry. São DUAS consultas guiadas
// por índice parcial (schemaV34) unidas, em vez de um OR sobre o histórico
// inteiro: o OR antigo forçava SCAN de runtime_orders a cada tick e, em
// produção, levava 14 s para devolver 4 linhas — dentro de transação de escrita.
//
// O ramo de retry força o índice parcial no SQLite (INDEXED BY): sem
// estatísticas o planner prefere idx_instances_status e percorre TODA instance
// WAITING — 28,8 mil linhas a cada tick na VPS (v0.2.72), 57% da CPU do server.
// No Postgres o planner tem estatísticas e a sintaxe não existe.
func activeResourceOrdersQuery(dialect db.Dialect) string {
	hint := ""
	if dialect == db.SQLite {
		hint = " INDEXED BY instances_retry_wait"
	}
	return "SELECT r.instance_id,o.current_execution,o.snapshot,o.snapshot_checksum FROM execution_attempts a JOIN lab_orders o ON o.id=a.order_id AND o.current_execution=a.execution_id JOIN runtime_orders r ON r.order_id=o.id WHERE " + db.ActiveAttemptPredicate("a") +
		" UNION SELECT r.instance_id,o.current_execution,o.snapshot,o.snapshot_checksum FROM instances i" + hint + " JOIN runtime_orders r ON r.instance_id=i.id JOIN lab_orders o ON o.id=r.order_id JOIN execution_attempts a ON a.execution_id=o.current_execution WHERE " + db.RetryWaitPredicate("i")
}

type resourceOrder struct {
	id, exec string
	needs    map[string]int
}

type rowQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	Dialect() db.Dialect
}

// readResourceOrders lê e valida as ordens vivas. Devolve também se alguma
// delas declara recurso — sem isso não há reserva a reconciliar.
func readResourceOrders(q rowQuerier) ([]resourceOrder, bool, error) {
	rows, err := q.Query(activeResourceOrdersQuery(q.Dialect()))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	records := []resourceOrder{}
	hasNeeds := false
	for rows.Next() {
		var r resourceOrder
		var raw, sum string
		if err = rows.Scan(&r.id, &r.exec, &raw, &sum); err != nil {
			return nil, false, err
		}
		var def domain.JobDefinition
		if digest(raw) != sum || json.Unmarshal([]byte(raw), &def) != nil {
			return nil, false, execution.ErrConflict
		}
		for name, qty := range def.Resources {
			if qty <= 0 {
				continue
			}
			if r.needs == nil {
				r.needs = map[string]int{}
			}
			r.needs[name] = qty
			hasNeeds = true
		}
		records = append(records, r)
	}
	return records, hasNeeds, rows.Err()
}

func (s *Scheduler) rebuildDurableResources() (int, error) {
	// Pré-leitura fora de transação: no caso comum (nenhuma ordem viva declara
	// recurso) o tick não abre transação de escrita nem toma o lock da fila —
	// era isso que fazia login, auditoria e poll do agente esperarem atrás do
	// scheduler. Havendo reserva a conferir, tudo é relido e validado dentro da
	// transação abaixo; a pré-leitura nunca decide uma escrita.
	records, hasNeeds, err := readResourceOrders(s.db)
	if err != nil {
		return 0, err
	}
	if !hasNeeds {
		return len(records), nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = tx.LockExecutionQueue(); err != nil {
		return 0, err
	}
	if err = s.guardLeadership(tx); err != nil {
		return 0, err
	}
	if records, _, err = readResourceOrders(tx); err != nil {
		return 0, err
	}
	for _, r := range records {
		for name, qty := range r.needs {
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
	if err = tx.LockExecutionQueue(); err != nil {
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
