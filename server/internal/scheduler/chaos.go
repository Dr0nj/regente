// Package scheduler — D-11 Chaos engineering ("Inject Failure").
//
// Injeta uma falha SINTÉTICA numa instance para testar a resiliência do
// workflow de verdade: o flip passa pelo FinishInstance REAL, então retry
// (P15/D-1), Actions On-Do, alerting (Phase 8) e carry-over reagem exatamente
// como numa falha de produção — é isso que se quer validar, não um mock.
//
// Segurança operacional: só WAITING/HELD/RUNNING são injetáveis (um job
// terminado não "re-falha"); a injeção fica registrada no event log da
// instance (kind="chaos", actor=usuário) — auditável e distinguível de uma
// falha orgânica pelo output "(chaos: ...)".
package scheduler

import (
	"fmt"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// InjectFailure força a instance a falhar como se o executor tivesse retornado
// erro. actor = username do operador (auditoria). Instances WAITING/HELD são
// finalizadas direto (nunca chegaram a rodar); RUNNING é derrubada — se o agente
// reportar o resultado real depois, ele é ignorado (a instance já saiu de RUNNING
// e o resultado tardio não re-flipa o estado terminal do chaos... a menos que o
// retry a tenha re-armado, que é o comportamento desejado do experimento).
func (s *Scheduler) InjectFailure(id, actor string) error {
	var status string
	if err := s.db.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status); err != nil {
		return fmt.Errorf("instance %s not found", id)
	}
	switch status {
	case string(domain.StatusWaiting), string(domain.StatusHeld), string(domain.StatusRunning):
	default:
		return fmt.Errorf("instance %s is %s; inject-failure only valid for WAITING/HELD/RUNNING", id, status)
	}
	s.emitEvent(id, "chaos", actor, "failure injected (chaos engineering)")
	// Fluxo REAL de falha: retries/Actions/alerts avaliados como numa falha orgânica.
	s.FinishInstance(id, domain.StatusNotOK, 137, "(chaos: failure injected by "+actor+")")
	return nil
}
