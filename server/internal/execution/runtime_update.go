package execution

import "github.com/Dr0nj/regente-server/internal/db"

func (e *Engine) RuntimeUpdate(instance string, fn func(*db.Tx, Order, Attempt) error) error {
	o, err := e.RuntimeOrder(instance)
	if err != nil {
		return err
	}
	tx, err := e.begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	o, err = lockOrder(tx, o.ID)
	if err != nil {
		return err
	}
	if o.CurrentExecution == "" {
		return ErrConflict
	}
	a, err := scanAttempt(tx.QueryRow(attemptSelect, o.CurrentExecution))
	if err != nil {
		return err
	}
	if !current(o, a) || a.State != "running" {
		return ErrConflict
	}
	if err = fn(tx, o, a.Attempt); err != nil {
		return err
	}
	return tx.Commit()
}
