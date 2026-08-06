// ST-1 — Execuções (2026-08-05): a EXECUÇÃO é um registro próprio (`instance_runs`,
// schemaV23), não uma inferência sobre `instances.started_at`/`finished_at`.
//
// Por que: o par de timestamps da instance é MUTADO por transições que não são
// execução. Um Set OK num WAITING que já tentou (o retry agendado zerou o
// finished_at) carimba finished_at=AGORA sobre um started_at de horas atrás; o
// retry e o cyclic sobrescrevem os timestamps a cada tentativa/volta (as
// anteriores somem); e um Set OK num WAITING que NUNCA rodou entrava na contagem
// de "runs" sem ter executado. A Statistics lia tudo isso como duração de
// execução — daí médias e máximos absurdos para jobs de duração fixa.
//
// A régua aqui é a do operador: começou = REIVINDICOU o RUNNING (a mesma
// transição que carimba started_at); ficar em WAIT não conta. Ação de operador
// sobre o que não está rodando não abre nem fecha execução.
//
// Ciclo de vida da linha:
//
//	recordRunStart  — claim WAITING→RUNNING (startInstance), abre a linha
//	recordRunEnd    — término da TENTATIVA (FinishInstance, inclusive quando o
//	                  retry re-arma; finishKilled no cancel de um RUNNING)
//	discardRunStart — o claim foi revertido sem executar (sem agente pra receber
//	                  o dispatch): a linha aberta some, como o started_at some.
package scheduler

import (
	"log"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// recordRunStart abre a execução da tentativa corrente. INSERT ... SELECT resolve
// definition_id/ODAT/attempt/started_at na própria query (mesmo padrão do
// insertOutput) — sem round-trip extra e sem divergir do started_at que o claim
// acabou de carimbar. Best-effort: falha loga e segue (estatística não pode
// derrubar dispatch).
func (s *Scheduler) recordRunStart(instanceID string) {
	_, err := s.db.Exec(
		`INSERT INTO instance_runs(instance_id, definition_id, order_date, attempt, started_at, status)
		 SELECT id, definition_id, `+odateExpr+`, COALESCE(attempts,1), started_at, ?
		   FROM instances WHERE id=? AND started_at IS NOT NULL`,
		string(domain.StatusRunning), instanceID,
	)
	if err != nil {
		log.Printf("[scheduler] recordRunStart %s: %v", instanceID, err)
	}
}

// recordRunEnd fecha a execução ABERTA mais recente da instance com o resultado
// da tentativa. Nada aberto = nada a fechar (Set OK/cancel de quem não estava
// rodando, resultado tardio já barrado pelo guard terminal) — silencioso de
// propósito: é a regra "só conta quem entrou em RUNNING", não um erro.
func (s *Scheduler) recordRunEnd(instanceID string, status domain.InstanceStatus, exitCode int) {
	_, err := s.db.Exec(
		`UPDATE instance_runs
		    SET finished_at=CURRENT_TIMESTAMP, status=?, exit_code=?,
		        agent_id=(SELECT agent_id FROM instances WHERE id=?)
		  WHERE id=(SELECT MAX(id) FROM instance_runs WHERE instance_id=? AND finished_at IS NULL)`,
		string(status), exitCode, instanceID, instanceID,
	)
	if err != nil {
		log.Printf("[scheduler] recordRunEnd %s: %v", instanceID, err)
	}
}

// discardRunStart apaga a execução aberta mais recente: o claim foi revertido
// ANTES de executar (dispatch sem agente online, que também zera o started_at da
// instance). Sem isso ficaria uma execução eternamente "em curso" que nunca rodou.
func (s *Scheduler) discardRunStart(instanceID string) {
	_, err := s.db.Exec(
		`DELETE FROM instance_runs
		  WHERE id=(SELECT MAX(id) FROM instance_runs WHERE instance_id=? AND finished_at IS NULL)`,
		instanceID,
	)
	if err != nil {
		log.Printf("[scheduler] discardRunStart %s: %v", instanceID, err)
	}
}
