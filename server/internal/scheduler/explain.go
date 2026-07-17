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

	// 2) Condições de entrada — o modelo ÚNICO de dependência: cada nome de
	// ConditionsIn precisa existir no POOL, no escopo resolvido do sufixo
	// @odat/@prev/@stat contra o ODAT (origem) da instance, não o order_date
	// avançado pelo carry-over. Quem cria: término OK/Set OK de quem tem a
	// condição na saída＋, ação On/Do set-condition, evento externo ou operador.
	// Quem apaga: saída− de um OK/Set OK, ou o operador (painel Condições).
	odate := r.Odate()
	if len(def.ConditionsIn) > 0 && s.conditions != nil {
		for _, m := range s.conditions.MissingIdx(def.ConditionsIn, odate, s.prevDaily, condIdx) {
			if add(Blocker{Kind: GateCondition, Condition: m.Name,
				Detail: "falta a condição '" + m.Name + "'" + m.ScopeLabel + " no pool (quem a cria precisa terminar OK — ou adicione no painel Condições)"}) {
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
		ex.Summary = "Run Now — despacho imediato (bypassa janela, condições e recursos)."
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
		ex.Summary = "Pronto pra rodar — todos os gates satisfeitos; despacho imediato."
	} else {
		ex.Summary = summarizeBlockers(blockers)
	}
	return ex, nil
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
