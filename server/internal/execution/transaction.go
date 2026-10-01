package execution

import "github.com/Dr0nj/regente-server/internal/db"

// I11: reservar o lock global antes da ordem em ambos os backends.
// Reconciliação, término, quota e admissão enxergam o mesmo corte transacional.
func (e *Engine) begin() (*db.Tx, error) {
	tx, err := e.DB.Begin()
	if err != nil {
		return nil, err
	}
	{
		if _, err = tx.Exec(`UPDATE execution_queue_lock SET marker=marker WHERE id=1`); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	return tx, nil
}
