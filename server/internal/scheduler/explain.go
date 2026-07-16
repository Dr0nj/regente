// Package scheduler — Explain ("por que esse job não rodou?").
//
// Diferencial sobre o Control-M (sem IA): o scheduler já COMPUTA o gating por
// instance; aqui ele EXPÕE o porquê. A peça central é gateInstance — a FONTE
// ÚNICA do gating: o tick a usa pra DECIDIR (sem bloqueios → reserva recurso +
// dispara) e o Explain a usa pra MOSTRAR. Como o dispatch passa por ela, nenhum
// gate pode bloquear sem aparecer no Explain → condição nova é absorvida de graça.
package scheduler

import (
	"fmt"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// GateKind — categoria de um bloqueio de gating (estável p/ UI e futuro MCP).
type GateKind string

const (
	GateWindow       GateKind = "WAIT_WINDOW"    // ainda não chegou o horário agendado
	GateWindowClosed GateKind = "WINDOW_CLOSED"  // a janela (WindowTo) já fechou hoje — não submete mais
	GateConfirm      GateKind = "WAIT_CONFIRM"   // Control-M Confirm: aguarda liberação do operador
	GateDep          GateKind = "WAIT_DEP"       // upstream ainda não satisfez a condição
	GateDepBlocked   GateKind = "BLOCKED_DEP"    // upstream tornou a dep impossível → CANCELLED
	GateCondition    GateKind = "WAIT_CONDITION" // falta uma condition IN (F16)
	GateAgent        GateKind = "WAIT_AGENT"     // nenhum agente online com a capability (ou o pinado offline)
	GateResource     GateKind = "WAIT_RESOURCE"  // recurso/quota indisponível (F15)
)

// Blocker — um motivo ATIVO de uma instance WAITING não estar rodando. Carrega
// texto legível + campos estruturados (p/ UI e futuro tool MCP explain_job()).
type Blocker struct {
	Kind      GateKind `json:"kind"`
	Detail    string   `json:"detail"`
	Upstream  string   `json:"upstream,omitempty"`
	UpStatus  string   `json:"upstreamStatus,omitempty"`
	Condition string   `json:"condition,omitempty"`
	Resource  string   `json:"resource,omitempty"`
	Want      int      `json:"want,omitempty"`
	Used      int      `json:"used,omitempty"`
	Capacity  int      `json:"capacity,omitempty"`
}

// Explanation — resposta do Explain por instance.
type Explanation struct {
	InstanceID string    `json:"instanceId"`
	DefID      string    `json:"definitionId"`
	Status     string    `json:"status"`
	Runnable   bool      `json:"runnable"` // WAITING e sem bloqueios → entra no próximo tick
	Summary    string    `json:"summary"`
	Blockers   []Blocker `json:"blockers"`
}

// edgeState — REGRA pura de UMA dependência upstream (Control-M condition).
// FONTE ÚNICA: usada pelo evalDeps (decisão do tick) e pelo gateInstance (Explain).
//
//	satisfied=true  → a aresta está OK
//	permanent=true  → o upstream tornou a aresta IMPOSSÍVEL (sucessor vira CANCELLED)
//	exists=false    → o upstream ainda não foi materializado neste order_date (espera)
func edgeState(cond domain.EdgeCondition, upStatus string, exists bool) (satisfied, permanent bool) {
	if !exists {
		return false, false
	}
	switch cond {
	// Condition VAZIA = on-success (default do produto, igual ao front). Antes
	// "" caía em on-complete — uma def YAML escrita à mão sem `condition:`
	// rodava o filho mesmo com o pai NOTOK. Default seguro: só sucesso libera.
	case domain.CondOnSuccess, "":
		if upStatus == string(domain.StatusOK) {
			return true, false
		}
		if upStatus == string(domain.StatusNotOK) || upStatus == string(domain.StatusCancelled) {
			return false, true
		}
		return false, false
	case domain.CondOnFailure:
		if upStatus == string(domain.StatusNotOK) {
			return true, false
		}
		if upStatus == string(domain.StatusOK) {
			return false, true
		}
		return false, false
	case domain.CondOnComplete:
		if upStatus == string(domain.StatusOK) || upStatus == string(domain.StatusNotOK) {
			return true, false
		}
		return false, false
	case domain.CondAlways:
		return true, false
	}
	return false, false
}

// gateInstance — FONTE ÚNICA do gating de uma instance WAITING. Avalia janela,
// deps, conditions e recursos (read-only) e devolve os bloqueios ATIVOS. Vazio =
// pode rodar. Ordem = a do tick (janela → deps → conditions → recursos).
//
// `claims` = latches de dependência desta instance (dep_claims, schemaV15):
// aresta clamada é satisfeita PARA SEMPRE para esta instance — rerun/cancel do
// upstream não a desfaz. Aresta sem claim espera um EVENTO livre (o tick tenta
// clamar no pré-passe; aqui o gate só lê). nil = sem latch conhecido.
//
// Instance FORÇADA (r.Forced) pula os gates de JANELA (1 e 1b): a ordem manual
// nasce elegível agora — mas deps/conditions/agente/recursos/Confirm valem
// (force_mode='order'; o Run Now nem passa por aqui).
//
// shortCircuit=true para no 1º bloqueio (hot path do tick, que só precisa de
// "bloqueado?" + saber se o 1º é dep permanente); false coleta TODOS (Explain).
// A checagem de recurso é read-only (Shortfalls); a reserva atômica (TryAcquire)
// fica no tick, depois deste gate passar.
func (s *Scheduler) gateInstance(r instRow, def domain.JobDefinition, instByDef instIndex, claims map[string]bool, now time.Time, shortCircuit bool) []Blocker {
	var out []Blocker
	// add anexa o bloqueio e devolve true quando o avaliador deve PARAR (short-circuit).
	add := func(b Blocker) bool {
		out = append(out, b)
		return shortCircuit
	}

	if !r.Forced {
		// 1) Janela — ainda não chegou o horário agendado.
		if now.Before(r.ScheduledAt) {
			if add(Blocker{Kind: GateWindow, Detail: "ainda não chegou o horário agendado (" + r.ScheduledAt.Format("15:04") + ")"}) {
				return out
			}
		}
		// 1b) Janela fechou (Control-M time window): passou de WindowTo, não submete
		// mais hoje. A instance morre na virada da daily (WAITING nunca-rodou).
		if hh, mm, okW := parseHHMM(def.Schedule.WindowTo); okW {
			if t, err := time.Parse("2006-01-02", r.OrderDate); err == nil {
				windowEnd := time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, time.Local)
				if now.After(windowEnd) {
					if add(Blocker{Kind: GateWindowClosed, Detail: "janela de execução fechou às " + def.Schedule.WindowTo}) {
						return out
					}
				}
			}
		}
	}
	// 1c) Confirmação do operador (Control-M "Wait for confirmation"): def com
	// confirm:true não roda até o Confirm — nem forçada (checado também no tick).
	if def.Confirm && !r.Confirmed {
		if add(Blocker{Kind: GateConfirm, Detail: "aguardando confirmação do operador (job exige Confirm)"}) {
			return out
		}
	}

	// 2) Dependências upstream — por EVENTO consumível (schemaV15). Satisfeita ⇔
	// claim latchado (ou 'always' com o upstream ordenado). Sem claim, o motivo
	// distingue: pai nunca terminou (espera) × pai terminou mas o evento já foi
	// consumido por OUTRA instance (espera um término NOVO) × pai terminou num
	// estado incompatível (rerun/Set OK destravam). A cópia do pai consultada é
	// a da diária RESOLVIDA pelo dateRef contra o ODAT do consumidor (a origem
	// da ordem — carry-over não muda de quem o job depende).
	odate := r.Odate()
	for _, u := range def.Upstream {
		if claims[u.From] {
			continue // latch: satisfeita para sempre para ESTA instance
		}
		wantDate, anyDate := s.resolveEdgeDate(odate, u.DateRef)
		dateLabel := ""
		if wantDate != odate && !anyDate {
			dateLabel = " da diária " + wantDate
		}
		up, exists := instByDef.lookup(u.From, wantDate, anyDate)
		upStatus := up.Status
		if !exists {
			upStatus = "ainda não ordenado"
		}
		if u.Condition == domain.CondAlways {
			if exists {
				continue // always: basta o upstream existir na diária resolvida
			}
			if add(Blocker{Kind: GateDep, Upstream: u.From, UpStatus: upStatus, Condition: string(u.Condition),
				Detail: fmt.Sprintf("aguardando upstream '%s'%s (%s, ainda não ordenado)", u.From, dateLabel, condLabel(u.Condition))}) {
				return out
			}
			continue
		}
		b := Blocker{Kind: GateDep, Upstream: u.From, UpStatus: upStatus, Condition: string(u.Condition)}
		liveSat, permanent := edgeState(u.Condition, up.Status, exists)
		switch {
		case !exists:
			b.Detail = fmt.Sprintf("aguardando upstream '%s'%s (%s, ainda não ordenado)", u.From, dateLabel, condLabel(u.Condition))
		case liveSat:
			// O estado vivo satisfaria, mas não há evento LIVRE: o término do pai
			// já foi consumido — por outra instance desta definition (cópia
			// forçada) ou pelo run ANTERIOR desta própria instance (rerun após
			// OK: consumo é permanente). Rerun no pai emite um evento novo.
			b.Detail = fmt.Sprintf("aguardando NOVO término de '%s'%s — o último evento já foi consumido por uma execução OK (rerun no upstream libera)", u.From, dateLabel)
		case permanent:
			b.Kind = GateDepBlocked
			b.Detail = fmt.Sprintf("upstream '%s'%s está %s — a aresta (%s) espera um término compatível (rerun/Set OK no upstream libera)", u.From, dateLabel, upStatus, condLabel(u.Condition))
		default:
			b.Detail = fmt.Sprintf("aguardando upstream '%s'%s (%s, está %s)", u.From, dateLabel, condLabel(u.Condition), upStatus)
		}
		if add(b) {
			return out
		}
	}

	// 3) Conditions IN (F16) — escopo pela diária resolvida do sufixo @odat/
	// @prev/@stat contra o ODAT (origem) da instance, não o order_date avançado.
	if len(def.ConditionsIn) > 0 && s.conditions != nil {
		for _, m := range s.conditions.Missing(def.ConditionsIn, odate, s.prevDaily) {
			if add(Blocker{Kind: GateCondition, Condition: m.Name, Detail: "falta a condition '" + m.Name + "'" + m.ScopeLabel}) {
				return out
			}
		}
	}

	// 4) Agente disponível — SEM agente com a capability (ou com o agente pinado
	//    offline), o job NÃO é reivindicado: fica WAITING ("WAIT AGENT" na UI),
	//    sem churn de estado (nada de RUNNING↔WAITING piscando a cada tick, nem
	//    spam de log/evento). Quando um agente conecta, o ws handler cutuca um
	//    Tick e o job dispara NA HORA. SSH é agentless; DemoMode dispensa (mock).
	if !s.agentAvailable(def) {
		detail := "nenhum agente online com a capability " + def.JobType
		if def.Environment != "" {
			// ADV-2 — roteamento por ambiente: o motivo precisa dizer o env, senão
			// o operador vê agentes online e não entende o WAIT_AGENT.
			detail += " no ambiente '" + def.Environment + "'"
		}
		if def.AgentID != "" {
			detail = "agente '" + def.AgentID + "' offline"
			if def.Environment != "" && s.hub.GetAgent(def.AgentID) != nil {
				detail = "agente '" + def.AgentID + "' está noutro ambiente (job exige '" + def.Environment + "')"
			}
		}
		if add(Blocker{Kind: GateAgent, Detail: detail}) {
			return out
		}
	}

	// 5) Recursos / quotas (F15) — read-only.
	if len(def.Resources) > 0 && s.resources != nil {
		for _, sf := range s.resources.Shortfalls(def.Resources) {
			if add(Blocker{
				Kind: GateResource, Resource: sf.Name, Want: sf.Want, Used: sf.Used, Capacity: sf.Capacity,
				Detail: fmt.Sprintf("recurso '%s' indisponível (quer %d; uso %d/%d)", sf.Name, sf.Want, sf.Used, sf.Capacity),
			}) {
				return out
			}
		}
	}

	return out
}

func condLabel(c domain.EdgeCondition) string {
	if c == "" {
		return string(domain.CondOnSuccess) // default do produto (= front)
	}
	return string(c)
}

// Explain monta a explicação de uma instance: para WAITING, roda gateInstance em
// modo coletar-tudo; para os demais estados, descreve a situação terminal/ativa.
func (s *Scheduler) Explain(instanceID string) (Explanation, error) {
	var r instRow
	var forcedInt, confirmedInt int
	err := s.db.QueryRow(
		`SELECT id, definition_id, order_date, status, scheduled_at, started_at, carried_at,
		        COALESCE(forced,0), COALESCE(force_mode,''), COALESCE(confirmed,0), COALESCE(carried_from,''), COALESCE(definition_snapshot,'')
		 FROM instances WHERE id=?`, instanceID,
	).Scan(&r.ID, &r.DefID, &r.OrderDate, &r.Status, &r.ScheduledAt, &r.StartedAt, &r.CarriedAt, &forcedInt, &r.ForceMode, &confirmedInt, &r.CarriedFrom, &r.Snapshot)
	if err != nil {
		return Explanation{}, err
	}
	r.Forced = forcedInt == 1
	r.Confirmed = confirmedInt == 1

	ex := Explanation{InstanceID: r.ID, DefID: r.DefID, Status: r.Status, Blockers: []Blocker{}}

	// Estados não-WAITING: não há gating a avaliar; descreve a situação.
	switch r.Status {
	case string(domain.StatusRunning):
		ex.Summary = "Em execução."
		if r.StartedAt.Valid {
			ex.Summary = "Em execução desde " + r.StartedAt.Time.Format("15:04:05") + "."
		}
		return ex, nil
	case string(domain.StatusOK):
		ex.Summary = "Concluído com sucesso (OK)."
		return ex, nil
	case string(domain.StatusNotOK):
		ex.Summary = "Terminou com falha (NOTOK) — rerun ou Set OK pra tratar."
		return ex, nil
	case string(domain.StatusCancelled):
		ex.Summary = "Cancelado (dependência impossível ou cancelado por operador)."
		return ex, nil
	case string(domain.StatusHeld):
		ex.Summary = "Em HOLD — segurado por operador; não roda até liberar."
		return ex, nil
	}

	// WAITING: avalia os gates.
	s.mu.Lock()
	defs := make(map[string]domain.JobDefinition, len(s.defs))
	for _, d := range s.defs {
		defs[d.ID] = d
	}
	s.mu.Unlock()

	def, ok := defForInstance(r, defs)
	if !ok {
		ex.Summary = "Sem definição carregada — não é materializável (def removida/desabilitada?)."
		return ex, nil
	}
	// "Run Now" (forced sem force_mode) bypassa todos os gates menos Confirm e
	// agente. O "Order Force" (force_mode='order') cai no gating normal abaixo —
	// só a janela é bypassada (r.Forced, dentro do gateInstance).
	if r.Forced && r.ForceMode != ForceModeOrder {
		if def.Confirm && !r.Confirmed {
			ex.Blockers = []Blocker{{Kind: GateConfirm, Detail: "aguardando confirmação do operador (job exige Confirm)"}}
			ex.Summary = "Force aguardando Confirm — a confirmação não é bypassada (Control-M)."
			return ex, nil
		}
		ex.Runnable = true
		ex.Summary = "Run Now — despacho imediato (bypassa janela, deps, conditions e recursos)."
		return ex, nil
	}

	// Latches de dependência (schemaV15) — inclui claim-on-observe: se há um
	// evento LIVRE do upstream, o Explain latcha agora, exatamente como o
	// pré-passe do tick faria ~2s depois (mesma fonte, nunca diverge).
	claimed := s.claimsFor(r.ID)
	s.claimDepEdges(r, def, claimed)

	blockers := s.gateInstance(r, def, s.loadUpstreamInsts(r.Odate(), def), claimed, time.Now(), false)
	if blockers == nil {
		// gateInstance devolve nil quando não há bloqueio; o JSON precisa ser []
		// (o front itera blockers direto — null quebrava a UI ao abrir o Explain
		// de um WAITING pronto pra rodar, na janela entre a ação e o tick).
		blockers = []Blocker{}
	}
	ex.Blockers = blockers
	if len(blockers) == 0 {
		ex.Runnable = true
		ex.Summary = "Pronto pra rodar — todos os gates satisfeitos; despacho imediato."
	} else {
		ex.Summary = summarizeBlockers(blockers)
	}
	return ex, nil
}

// loadUpstreamInsts carrega SÓ as instances dos upstreams da def, nas diárias
// RESOLVIDAS pelos dateRefs contra o ODAT do consumidor (agregadas por
// (def, odate), como o tick) — Explain custa o mesmo independente do volume da
// daily (100k–1M). Arestas stat derrubam o filtro de data (bounded no LIMIT:
// só o melhor rank por def importa e instances recentes vêm primeiro).
func (s *Scheduler) loadUpstreamInsts(odate string, def domain.JobDefinition) instIndex {
	ix := instIndex{byDefDate: map[string]instRow{}, byDef: map[string]instRow{}}
	if len(def.Upstream) == 0 {
		return ix
	}
	ids := make(map[string]bool, len(def.Upstream))
	dates := make(map[string]bool, 2)
	hasStat := false
	for _, u := range def.Upstream {
		ids[u.From] = true
		if d, anyDate := s.resolveEdgeDate(odate, u.DateRef); anyDate {
			hasStat = true
		} else {
			dates[d] = true
		}
	}
	args := make([]any, 0, len(ids)+len(dates))
	ph := make([]string, 0, len(ids))
	for id := range ids {
		ph = append(ph, "?")
		args = append(args, id)
	}
	q := `SELECT definition_id, status, order_date, COALESCE(carried_from,'') FROM instances
	      WHERE definition_id IN (` + strings.Join(ph, ",") + `)`
	if !hasStat {
		dph := make([]string, 0, len(dates))
		for d := range dates {
			dph = append(dph, "?")
			args = append(args, d)
		}
		q += ` AND ` + odateExpr + ` IN (` + strings.Join(dph, ",") + `)`
	}
	q += ` ORDER BY order_date DESC LIMIT 500`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return ix
	}
	defer rows.Close()
	for rows.Next() {
		var r instRow
		if rows.Scan(&r.DefID, &r.Status, &r.OrderDate, &r.CarriedFrom) != nil {
			continue
		}
		ix.add(r)
	}
	return ix
}

func summarizeBlockers(bs []Blocker) string {
	if len(bs) == 1 {
		return "Não roda: " + bs[0].Detail + "."
	}
	parts := make([]string, 0, len(bs))
	for _, b := range bs {
		parts = append(parts, b.Detail)
	}
	return "Não roda — bloqueado por: " + strings.Join(parts, "; ") + "."
}
