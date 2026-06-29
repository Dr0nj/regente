// Package scheduler — Actions / On-Do (Control-M "On/Do").
//
// Motor de regras configuráveis POR JOB que reagem ao ciclo de execução, em três
// dimensões de gatilho (ver domain.ActionRule):
//
//	result   → status terminal do job (OK | NOTOK), após esgotar os retries.
//	attempt  → a N-ésima tentativa FALHOU (escada de rerun; cada tentativa falha).
//	runtime  → o job está RUNNING há mais que N minutos (shout por duração).
//
// E quatro ações, todas reusando substratos já existentes (sem duplicar lógica):
//
//	notify        → AlertEngine.FireAction  (sino/toast + Slack/webhook/e-mail/PagerDuty)
//	set-condition → ConditionEngine.Set     (destrava sucessores no escopo do order_date)
//	run-job       → Scheduler.ForceOrder    (roda outro job ignorando deps)
//	set-ok        → Scheduler.SetOK          (auto-heal: flipa o próprio NOTOK→OK)
//
// IDEMPOTÊNCIA: cada regra dispara no máximo uma vez por instance. A chave é o
// ÍNDICE da regra no array do job (regras distintas com o mesmo gatilho não se
// suprimem); o ledger durável `action_fires` (PK instance_id+action_key) garante
// "uma vez" mesmo entre ticks e através de restart. O claim é check-then-insert
// (o tick roda só no líder, single-threaded; a transição terminal é única) com a
// PK como rede de segurança.
package scheduler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// actionEvent — ocorrência avaliada contra as regras On/Do de um job.
type actionEvent struct {
	kind    string                // "result" | "attempt" | "runtime" (== ActionRule.On)
	status  domain.InstanceStatus // kind=="result"
	attempt int                   // kind=="attempt": nº (1-based) da tentativa que falhou
	runMin  int                   // kind=="runtime": minutos em RUNNING
}

// actionMatches — DECISOR PURO (testável sem DB): a regra dispara para o evento?
// Fonte única do "quando dispara"; usada pelo applyActions e pelos testes.
func actionMatches(rule domain.ActionRule, ev actionEvent) bool {
	if !strings.EqualFold(strings.TrimSpace(rule.On), ev.kind) {
		return false
	}
	switch ev.kind {
	case "result":
		return strings.EqualFold(strings.TrimSpace(rule.Status), string(ev.status))
	case "attempt":
		return rule.Attempt > 0 && rule.Attempt == ev.attempt
	case "runtime":
		return rule.AfterMin > 0 && ev.runMin >= rule.AfterMin
	}
	return false
}

// applyActions avalia TODAS as regras do job contra o evento e executa as que
// casam (uma vez cada, via claim no ledger). Best-effort: nunca propaga erro nem
// quebra o fluxo de finish/tick.
func (s *Scheduler) applyActions(instanceID, orderDate string, def domain.JobDefinition, ev actionEvent) {
	for i, rule := range def.Actions {
		if !actionMatches(rule, ev) {
			continue
		}
		if !s.claimActionFire(instanceID, strconv.Itoa(i)) {
			continue // já disparou (ledger) — idempotente
		}
		s.execAction(instanceID, orderDate, def, rule)
	}
}

// claimActionFire registra o disparo (instance, chave-da-regra) e devolve true só
// na PRIMEIRA vez. check-then-insert portável (SQLite/PG); a PK é a rede contra
// um raro insert duplo (erro ignorado → não dispara de novo).
func (s *Scheduler) claimActionFire(instanceID, key string) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM action_fires WHERE instance_id=? AND action_key=?`, instanceID, key).Scan(&n)
	if n > 0 {
		return false
	}
	if _, err := s.db.Exec(`INSERT INTO action_fires(instance_id, action_key) VALUES(?,?)`, instanceID, key); err != nil {
		return false
	}
	return true
}

// execAction executa UMA ação já decidida (gatilho casou + claim ok).
func (s *Scheduler) execAction(instanceID, orderDate string, def domain.JobDefinition, rule domain.ActionRule) {
	switch strings.TrimSpace(rule.Do) {
	case "notify":
		if s.alerts == nil {
			return
		}
		msg := rule.Message
		if msg == "" {
			msg = defaultActionMessage(def, rule)
		}
		s.alerts.FireAction(def.ID, labelOf(def), "On/Do · "+labelOf(def), rule.Severity, msg, rule.Channels)
		s.emitEvent(instanceID, "action", "actions", "notify: "+msg)
	case "set-condition":
		if s.conditions == nil || strings.TrimSpace(rule.Condition) == "" {
			return
		}
		_ = s.conditions.Set(rule.Condition, orderDate, "actions")
		s.hub.BroadcastWeb("condition.changed", map[string]string{"name": rule.Condition, "scopeDate": orderDate})
		s.emitEvent(instanceID, "action", "actions", "set-condition: "+rule.Condition)
	case "run-job":
		if strings.TrimSpace(rule.TargetJob) == "" {
			return
		}
		newID, err := s.ForceOrder(rule.TargetJob)
		if err != nil {
			s.emitEvent(instanceID, "action", "actions", "run-job FALHOU ("+rule.TargetJob+"): "+err.Error())
			return
		}
		s.emitEvent(instanceID, "action", "actions", "run-job: "+rule.TargetJob+" → "+newID)
	case "set-ok":
		if err := s.SetOK(instanceID); err != nil {
			return // só vale NOTOK/CANCELLED; ignora silenciosamente fora disso
		}
		s.emitEvent(instanceID, "action", "actions", "set-ok (auto-heal)")
	}
}

// defaultActionMessage — texto padrão de um notify sem Message explícita.
func defaultActionMessage(def domain.JobDefinition, rule domain.ActionRule) string {
	switch strings.ToLower(rule.On) {
	case "result":
		return fmt.Sprintf("%s terminou %s", labelOf(def), strings.ToUpper(rule.Status))
	case "attempt":
		return fmt.Sprintf("%s falhou na tentativa %d", labelOf(def), rule.Attempt)
	case "runtime":
		return fmt.Sprintf("%s rodando há mais de %dmin", labelOf(def), rule.AfterMin)
	}
	return labelOf(def) + ": ação disparada"
}

// labelOf — rótulo amigável do job (Label, ou ID se vazio).
func labelOf(def domain.JobDefinition) string {
	if def.Label != "" {
		return def.Label
	}
	return def.ID
}

// instanceContext — def CONGELADA (snapshot imutável do momento da ordem) +
// order_date + nº da tentativa atual de uma instance. Fonte para avaliar as
// Actions: as regras são as que valiam quando o job foi ordenado, não as atuais.
// ok=false se a instance sumiu ou não tem snapshot (instances pré-snapshot).
func (s *Scheduler) instanceContext(id string) (def domain.JobDefinition, orderDate string, attempt int, ok bool) {
	var snapshot string
	if err := s.db.QueryRow(
		`SELECT COALESCE(definition_snapshot,''), order_date, COALESCE(attempts,1) FROM instances WHERE id=?`, id,
	).Scan(&snapshot, &orderDate, &attempt); err != nil || snapshot == "" {
		return domain.JobDefinition{}, "", 0, false
	}
	if json.Unmarshal([]byte(snapshot), &def) != nil {
		return domain.JobDefinition{}, "", 0, false
	}
	return def, orderDate, attempt, true
}

// evaluateRuntimeActions — dimensão "runtime": para cada instance RUNNING com
// regras On=="runtime", dispara as que cruzaram o limiar de minutos. Chamada a
// cada tick (junto do SLA), varrendo só o conjunto RUNNING (limitado pela
// concorrência, barato mesmo a 1M/dia). A duração é medida de started_at (início
// real da execução). Idempotente pelo ledger (cada limiar dispara uma vez).
func (s *Scheduler) evaluateRuntimeActions(now time.Time) {
	rows, err := s.db.Query(
		`SELECT id, order_date, started_at, COALESCE(definition_snapshot,'') FROM instances
		 WHERE status='RUNNING' AND started_at IS NOT NULL`,
	)
	if err != nil {
		return
	}
	defer rows.Close()
	type pending struct {
		id, orderDate string
		def           domain.JobDefinition
		runMin        int
	}
	var todo []pending
	for rows.Next() {
		var id, orderDate, snapshot string
		var started time.Time
		if rows.Scan(&id, &orderDate, &started, &snapshot) != nil || snapshot == "" {
			continue
		}
		var def domain.JobDefinition
		if json.Unmarshal([]byte(snapshot), &def) != nil || !hasRuntimeAction(def) {
			continue
		}
		todo = append(todo, pending{id, orderDate, def, int(now.Sub(started).Minutes())})
	}
	// Aplica fora do cursor (applyActions escreve no ledger).
	for _, p := range todo {
		s.applyActions(p.id, p.orderDate, p.def, actionEvent{kind: "runtime", runMin: p.runMin})
	}
}

func hasRuntimeAction(def domain.JobDefinition) bool {
	for _, r := range def.Actions {
		if strings.EqualFold(r.On, "runtime") {
			return true
		}
	}
	return false
}
