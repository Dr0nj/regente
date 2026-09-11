package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
)

type assigningBus interface {
	DispatchWithAssignment(string, string, string, []byte, func(string) error) (hub.DispatchOutcome, string)
}

// Implementações reais (hub/NATS) gravam a atribuição antes da entrega.
// O fallback preserva os doubles de Bus usados nos testes de semântica legados.
func (s *Scheduler) dispatchAssigned(id, agentID, capability, env string, raw []byte) (hub.DispatchOutcome, string) {
	assign := func(agent string) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		res, err := s.db.ExecContext(ctx, `UPDATE instances SET agent_id=? WHERE id=? AND status='RUNNING'`, agent, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return errors.New("instance no longer running")
		}
		return nil
	}
	if b, ok := s.hub.(assigningBus); ok {
		return b.DispatchWithAssignment(agentID, capability, env, raw, assign)
	}
	out, agent := s.hub.Dispatch(agentID, capability, env, raw)
	if out == hub.DispatchSent {
		if err := assign(agent); err != nil {
			return hub.DispatchNoAgent, ""
		}
	}
	return out, agent
}
