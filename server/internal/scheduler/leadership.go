package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/execution"
)

// Recibos de agentes e operações humanas podem chegar no follower. Só decisões
// de agendamento exigem o termo; o próprio commit serializa a troca de líder.
func (s *Scheduler) guardLeadership(tx *db.Tx) error {
	if s.leader == nil {
		return nil
	}
	if g, ok := s.leader.(interface{ Guard(*db.Tx) error }); ok {
		return g.Guard(tx)
	}
	if !s.leader.IsLeader() {
		return execution.ErrConflict
	}
	return nil
}
func (s *Scheduler) leaderExec(query string, args ...any) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if s.db.Dialect() == db.SQLite {
		if _, err = tx.Exec("UPDATE execution_queue_lock SET marker=marker WHERE id=1"); err != nil {
			return err
		}
	}
	if err = s.guardLeadership(tx); err != nil {
		return err
	}
	if _, err = tx.Exec(query, args...); err != nil {
		return err
	}
	return tx.Commit()
}
