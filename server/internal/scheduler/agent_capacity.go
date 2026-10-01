package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/execution"
	"time"
)

// Informações do processo autenticado; reduções não cancelam reservas existentes.
func (s *Scheduler) ReportAgentCapacity(agent string, slots, pending int, available bool) error {
	if agent == "" || slots < 1 || slots > 10000 || pending < 1 || pending > 100000 {
		return execution.ErrInvalid
	}
	yes := 0
	if available {
		yes = 1
	}
	_, err := s.db.Exec("INSERT INTO execution_agent_capacity(agent_id,slots,pending_limit,available,last_seen) VALUES(?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET slots=excluded.slots,pending_limit=excluded.pending_limit,available=excluded.available,last_seen=excluded.last_seen", agent, slots, pending, yes, time.Now().UnixMilli())
	return err
}
func (s *Scheduler) durableAgentAdmission(tx *db.Tx, agent string) error {
	var slots, pending, available, used int
	var seen int64
	if err := tx.QueryRow("SELECT slots,pending_limit,available,last_seen FROM execution_agent_capacity WHERE agent_id=?", agent).Scan(&slots, &pending, &available, &seen); err != nil {
		return err
	}
	if err := tx.QueryRow("SELECT COUNT(*) FROM execution_attempts WHERE agent_id=? AND state NOT IN ('succeeded','failed','cancelled')", agent).Scan(&used); err != nil {
		return err
	}
	// Esta tentativa já foi inserida na mesma transação. Não há oversubscription.
	if available != 1 || seen < time.Now().Add(-15*time.Second).UnixMilli() || used > slots || used > pending {
		return execution.ErrCapacity
	}
	return nil
}
