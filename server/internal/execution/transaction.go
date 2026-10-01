package execution

import "github.com/Dr0nj/regente-server/internal/db"

// SQLite precisa reservar writer ANTES de qualquer SELECT, evitando upgrade
// de snapshot de leitura concorrente (SQLITE_BUSY). PG usa o lock da ordem.
func (e *Engine) begin() (*db.Tx, error) {
	tx, err := e.DB.Begin()
	if err != nil {
		return nil, err
	}
	if e.DB.Dialect() == db.SQLite {
		if _, err = tx.Exec(`UPDATE execution_queue_lock SET marker=marker WHERE id=1`); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	return tx, nil
}
