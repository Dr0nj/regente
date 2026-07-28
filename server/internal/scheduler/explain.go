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
	GateCondition    GateKind = "WAIT_CONDITION" // falta uma condição de entrada no pool (modelo único)
	GateAgent        GateKind = "WAIT_AGENT"     // nenhum agente online com a capability (ou o pinado offline)
	GateResource     GateKind = "WAIT_RESOURCE"  // recurso/quota indisponível (F15)
)

// Blocker — um motivo ATIVO de uma instance WAITING não estar rodando. Carrega
// texto legível + campos estruturados (p/ UI e futuro tool MCP explain_job()).
type Blocker struct {
	Kind      GateKind `json:"kind"`
	Detail    string   `json:"detail"`
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

// gateInstance — FONTE ÚNICA do gating de uma instance WAITING. Avalia janela,
// condições e recursos (read-only) e devolve os bloqueios ATIVOS. Vazio =
// pode rodar. Ordem = a do tick (janela → confirm → condições → agente → recursos).
//
// CONDIÇÕES são o modelo ÚNICO de dependência (2026-07-17): o job espera as
// ConditionsIn existirem no POOL, no escopo resolvido de cada sufixo contra o
// seu ODAT. A setinha do grafo é só açúcar que escreve condições — não existe
// gate separado de "upstream". `condIdx` é a foto do pool pré-carregada pelo
// tick (nil = consulta direta, caminho do Explain/Force).
//
// Instance FORÇADA (r.Forced) pula os gates de JANELA (1 e 1b): a ordem manual
// nasce elegível agora — mas condições/agente/recursos/Confirm valem
// (force_mode='order'; o Run Now nem passa por aqui).
//
// shortCircuit=true para no 1º bloqueio (hot path do tick); false coleta
// TODOS (Explain). A checagem de recurso é read-only (Shortfalls); a reserva
// atômica (TryAcquire) fica no tick, depois deste gate passar.
func (s *Scheduler) gateInstance(r instRow, def domain.JobDefinition, condIdx CondIndex, now time.Time, shortCircuit bool) []Blocker {
	var out []Blocker
	// add anexa o bloqueio e devolve true quando o avaliador deve PARAR (short-circuit).
	add := func(b Blocker) bool {
		out = append(out, b)
		return shortCircuit
	}

	// "Run Now" clássico (forced sem force_mode) ignora TUDO — inclusive a janela
	// de horário. Já o "Order Force" do Design (force_mode='order') é uma ordem NOVA
	// fora do AGENDAMENTO, mas respeita os gates de RUNTIME; a janela de horário é
	// um deles (paridade Control-M: um force-order espera o "From Time" e não submete
	// passado o "To Time"). Por isso o gate de janela vale pro Order Force também;
	// só o total-bypass do Run Now escapa.
	totalBypass := r.Forced && r.ForceMode != ForceModeOrder
	// CL-2: quando a lógica de entrada usa o token $TIME, o piso de JANELA (gates
	// 1 e 1a) deixa de valer — a trava de início vira SÓ o $TIME dentro da
	// expressão (`(cond) OU ($TIME)`), senão o OR nunca anteciparia por condição
	// antes do WindowFrom. O TETO (1b, WindowTo/WINDOW_CLOSED) continua SEMPRE.
	timeInLogic := def.ConditionLogic.UsesTimeToken()
	if !totalBypass {
		// 1) Janela — ainda não chegou o horário agendado (scheduled_at). Num daily
		// o scheduled_at JÁ é o WindowFrom (computeScheduledAt), então este teste
		// basta e o carry-over segue intocado. Pulado quando o $TIME está na lógica.
		if !timeInLogic && now.Before(r.ScheduledAt) {
			if add(Blocker{Kind: GateWindow, Detail: "the scheduled time has not arrived yet (" + r.ScheduledAt.Format("15:04") + ")"}) {
				return out
			}
		}
		// 1a) Início da janela p/ Order Force: seu scheduled_at é "now" (ordem fora
		// do agendamento), então o WindowFrom NÃO está embutido no scheduled_at —
		// aplicamos a trava de início explicitamente só aqui, sem tocar no daily.
		// Também pulado quando o $TIME está na lógica (o token cobre o início —
		// e num Order Force o próprio $TIME usa esta trava, ver orderForceWindowStart).
		if !timeInLogic && r.Forced && r.ForceMode == ForceModeOrder {
			if ws, ok := orderForceWindowStart(def, r.OrderDate); ok && now.Before(ws) {
				if add(Blocker{Kind: GateWindow, Detail: "the execution window opens at " + def.Schedule.WindowFrom}) {
					return out
				}
			}
		}
		// 1b) Janela fechou (Control-M time window): passou de WindowTo, não submete
		// mais hoje. A instance morre na virada da daily (WAITING nunca-rodou).
		if hh, mm, okW := parseHHMM(def.Schedule.WindowTo); okW {
			if t, err := time.Parse("2006-01-02", r.OrderDate); err == nil {
				windowEnd := time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, time.Local)
				if now.After(windowEnd) {
					if add(Blocker{Kind: GateWindowClosed, Detail: "the execution window closed at " + def.Schedule.WindowTo}) {
						return out
					}
				}
			}
		}
	}
	// 1c) Confirmação do operador (Control-M "Wait for confirmation"): def com
	// confirm:true não roda até o Confirm — nem forçada (checado também no tick).
	if def.Confirm && !r.Confirmed {
		if add(Blocker{Kind: GateConfirm, Detail: "waiting for operator confirmation (the job requires Confirm)"}) {
			return out
		}
	}

	// 2) Condições de entrada — o modelo ÚNICO de dependência: cada nome de
	// ConditionsIn precisa existir no POOL, no escopo resolvido do sufixo
	// @odat/@prev/@stat contra o ODAT (origem) da instance, não o order_date
	// avançado pelo carry-over. Quem cria: término OK/Set OK de quem tem a
	// condição na saída＋, ação On/Do set-condition, evento externo ou operador.
	// Quem apaga: saída− de um OK/Set OK, ou o operador (painel Condições).
	//
	// CL: quando a def tem lógica booleana (ConditionLogic — grupos AND/OR em
	// forma DNF), o gate avalia a EXPRESSÃO contra o pool + o token $TIME e
	// bloqueia só se NENHUM ramo é satisfazível (um blocker que descreve a
	// expressão). Sem lógica (nil) é o AND implícito de todas as ConditionsIn —
	// caminho clássico, um blocker por condição faltante (inalterado).
	odate := r.Odate()
	if s.conditions != nil && def.ConditionLogic != nil && len(def.ConditionLogic.Groups) > 0 {
		// $TIME = o "a partir de" (windowFrom). No daily o WindowFrom já está
		// embutido no scheduled_at (computeScheduledAt), então o token lê dali.
		// Num ORDER FORCE o scheduled_at é "now" (ordem fora do agendamento) —
		// ler dali tornaria o $TIME verdadeiro na hora e "(cond) OU horário"
		// forçado furaria o WindowFrom, que o Order Force respeita. Usa a MESMA
		// trava explícita do gate 1a (WindowFrom × order_date).
		timeReady := !now.Before(r.ScheduledAt)
		if r.Forced && r.ForceMode == ForceModeOrder {
			if ws, ok := orderForceWindowStart(def, r.OrderDate); ok {
				timeReady = !now.Before(ws)
			}
		}
		sat := func(m string) bool {
			if m == domain.CondTokenTime {
				return timeReady
			}
			base, scope := resolveCondScope(m, odate, s.prevDaily)
			return s.conditions.hasIdx(base, scope, condIdx)
		}
		if ev := domain.EvalConditionLogic(def.ConditionLogic, def.ConditionsIn, sat); !ev.Satisfied {
			if add(Blocker{Kind: GateCondition,
				Detail: "waiting for " + ev.RenderExpr() + " — no branch satisfied in the pool"}) {
				return out
			}
		}
	} else if len(def.ConditionsIn) > 0 && s.conditions != nil {
		for _, m := range s.conditions.MissingIdx(def.ConditionsIn, odate, s.prevDaily, condIdx) {
			if add(Blocker{Kind: GateCondition, Condition: m.Name,
				Detail: "the condition '" + m.Name + "'" + m.ScopeLabel + " is missing from the pool (whoever creates it must end OK — or add it in the Conditions panel)"}) {
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
		detail := "no agent online with the " + def.JobType + " capability"
		if def.Environment != "" {
			// ADV-2 — roteamento por ambiente: o motivo precisa dizer o env, senão
			// o operador vê agentes online e não entende o WAIT_AGENT.
			detail += " in the '" + def.Environment + "' environment"
		}
		if def.AgentID != "" {
			detail = "agent '" + def.AgentID + "' is offline"
			if def.Environment != "" && s.hub.GetAgent(def.AgentID) != nil {
				detail = "agent '" + def.AgentID + "' is in another environment (the job requires '" + def.Environment + "')"
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
				Detail: fmt.Sprintf("resource '%s' unavailable (wants %d; used %d/%d)", sf.Name, sf.Want, sf.Used, sf.Capacity),
			}) {
				return out
			}
		}
	}

	return out
}

// orderForceWindowStart — instante em que a janela (WindowFrom) abre no dia da
// ordem. É a trava de início do ORDER FORCE, cujo scheduled_at é "now" e portanto
// NÃO embute o WindowFrom como no daily. Compartilhada pelo gate 1a e pelo token
// $TIME (CL-2) — os dois têm que enxergar o MESMO piso, senão "(cond) OU horário"
// forçado fura a janela que o Order Force respeita. ok=false quando a def não tem
// WindowFrom válido (sem janela = sem trava).
func orderForceWindowStart(def domain.JobDefinition, orderDate string) (time.Time, bool) {
	hh, mm, okW := parseHHMM(def.Schedule.WindowFrom)
	if !okW {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", orderDate)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, time.Local), true
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
		ex.Summary = "Running."
		if r.StartedAt.Valid {
			ex.Summary = "Running since " + r.StartedAt.Time.Format("15:04:05") + "."
		}
		return ex, nil
	case string(domain.StatusOK):
		ex.Summary = "Completed successfully (OK)."
		return ex, nil
	case string(domain.StatusNotOK):
		ex.Summary = "Ended with a failure (NOTOK) — rerun or Set OK to handle it."
		return ex, nil
	case string(domain.StatusCancelled):
		ex.Summary = "Cancelled (impossible dependency or cancelled by an operator)."
		return ex, nil
	case string(domain.StatusHeld):
		ex.Summary = "On HOLD — held by an operator; does not run until released."
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
		ex.Summary = "No definition loaded — not materializable (def removed/disabled?)."
		return ex, nil
	}
	// "Run Now" (forced sem force_mode) bypassa todos os gates menos Confirm e
	// agente. O "Order Force" (force_mode='order') cai no gating normal abaixo —
	// só a janela é bypassada (r.Forced, dentro do gateInstance).
	if r.Forced && r.ForceMode != ForceModeOrder {
		if def.Confirm && !r.Confirmed {
			ex.Blockers = []Blocker{{Kind: GateConfirm, Detail: "waiting for operator confirmation (the job requires Confirm)"}}
			ex.Summary = "Force waiting on Confirm — the confirmation is not bypassed."
			return ex, nil
		}
		ex.Runnable = true
		ex.Summary = "Run Now — immediate dispatch (bypasses window, conditions and resources)."
		return ex, nil
	}

	blockers := s.gateInstance(r, def, nil, time.Now(), false)
	if blockers == nil {
		// gateInstance devolve nil quando não há bloqueio; o JSON precisa ser []
		// (o front itera blockers direto — null quebrava a UI ao abrir o Explain
		// de um WAITING pronto pra rodar, na janela entre a ação e o tick).
		blockers = []Blocker{}
	}
	ex.Blockers = blockers
	if len(blockers) == 0 {
		ex.Runnable = true
		ex.Summary = "Ready to run — all gates satisfied; immediate dispatch."
	} else {
		ex.Summary = summarizeBlockers(blockers)
	}
	return ex, nil
}

func summarizeBlockers(bs []Blocker) string {
	if len(bs) == 1 {
		return "Does not run: " + bs[0].Detail + "."
	}
	parts := make([]string, 0, len(bs))
	for _, b := range bs {
		parts = append(parts, b.Detail)
	}
	return "Does not run — blocked by: " + strings.Join(parts, "; ") + "."
}
