package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// ControlPlaneMonitor (R7) — auto-SLO do control plane. O próprio servidor vigia a
// sua saúde e dispara alertas (pelos MESMOS canais das regras, via AlertEngine) quando
// o "cérebro" degrada:
//
//	tick parado        → o loop de scheduling não avança (ticker morto / cron parado)
//	DB inacessível     → o state store não responde
//	leader flapping    → a liderança troca demais numa janela (instabilidade de HA)
//	frota de agentes ↓  → agentes online caíram (capacidade de execução reduzida)
//	agente travado     → slots de um agente presos em execuções UNCERTAIN
//
// Só o LÍDER publica (evita N cópias do mesmo alerta no cluster); a exceção é o DB
// inacessível, que é logado em qualquer nó (e a publicação, se houver, é best-effort).
type ControlPlaneMonitor struct {
	alerts       *AlertEngine
	lastTick     func() time.Time
	isLeader     func() bool
	pingDB       func(context.Context) error
	onlineAgents func() int
	// admissions — ocupação de slots por agente (opcional; ver SetAgentAdmissions).
	admissions func() []AgentAdmission

	interval   time.Duration
	tickStale  time.Duration
	cooldownMs int64
	flapWindow time.Duration
	flapMax    int

	st cpState
}

// NewControlPlaneMonitor monta o monitor. interval = cadência da varredura;
// tickStale = idade do último tick acima da qual alerta. Demais limiares têm
// defaults sãos (flap: >5 trocas em 5min; cooldown de 5min por sinal).
func NewControlPlaneMonitor(
	alerts *AlertEngine,
	lastTick func() time.Time,
	isLeader func() bool,
	pingDB func(context.Context) error,
	onlineAgents func() int,
	interval, tickStale time.Duration,
) *ControlPlaneMonitor {
	return &ControlPlaneMonitor{
		alerts: alerts, lastTick: lastTick, isLeader: isLeader,
		pingDB: pingDB, onlineAgents: onlineAgents,
		interval: interval, tickStale: tickStale,
		cooldownMs: 5 * 60 * 1000,
		flapWindow: 5 * time.Minute,
		flapMax:    5,
	}
}

// AgentAdmission — ocupação dos slots de um agente na execução durável.
type AgentAdmission struct {
	AgentID                 string
	Slots, InUse, Uncertain int
}

// SetAgentAdmissions liga o sinal "agente travado". Produção 2026-11-13: um
// agente de 4 slots ficou três dias com 4 execuções UNCERTAIN, nenhum job foi
// despachado e nenhum alerta saiu — o estado só aparecia para quem abrisse o job.
func (m *ControlPlaneMonitor) SetAgentAdmissions(read func() []AgentAdmission) { m.admissions = read }

// Start sobe o loop de varredura em goroutine até o ctx ser cancelado.
func (m *ControlPlaneMonitor) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(m.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				m.runOnce(now)
			}
		}
	}()
}

// runOnce coleta as leituras, avalia e publica. Nunca derruba o processo.
func (m *ControlPlaneMonitor) runOnce(now time.Time) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[selfmon] PANIC recuperado: %v", r)
		}
	}()

	r := cpReading{tickAge: -1, leaderNow: m.isLeader(), agentsOnline: m.onlineAgents()}
	if last := m.lastTick(); !last.IsZero() {
		r.tickAge = now.Sub(last)
	}
	if m.admissions != nil {
		r.admissions = m.admissions()
	}
	pctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	r.dbErr = m.pingDB(pctx) != nil
	cancel()

	leader := r.leaderNow
	for _, a := range m.st.eval(now, r, m.tickStale, m.flapWindow, m.flapMax) {
		// DB caiu: loga sempre (garantia mesmo sem líder/sem DB p/ persistir o evento).
		if a.Key == "db-unreachable" {
			log.Printf("[selfmon] %s: %s", a.Name, a.Message)
		}
		if !leader {
			continue
		}
		cooldown := m.cooldownMs
		if long := sysAlertCooldown(a.Key); long > 0 {
			cooldown = long
		}
		m.alerts.FireSystem(a.Key, a.Name, a.Severity, a.Message, cooldown)
	}
}

// cpReading — leitura instantânea da saúde do control plane.
type cpReading struct {
	tickAge      time.Duration // desde o último tick; < 0 se ainda não ticou
	dbErr        bool
	leaderNow    bool
	agentsOnline int
	admissions   []AgentAdmission
}

// cpState — memória entre varreduras (transições de líder, baseline de agentes).
type cpState struct {
	hasPrevLeader bool
	prevLeader    bool
	leaderFlips   []time.Time
	hasPrevAgents bool
	prevAgents    int
}

// sysAlert — alerta do control plane a publicar.
type sysAlert struct {
	Key, Name, Severity, Message string
}

// sysAlertCooldown — condições que só um operador desfaz usam janela longa:
// repetir a cada 5 min vira ruído. 0 = cooldown padrão do monitor.
func sysAlertCooldown(key string) int64 {
	switch {
	case strings.HasPrefix(key, "agent-blocked:"):
		return int64(time.Hour / time.Millisecond)
	case strings.HasPrefix(key, "agent-uncertain:"):
		return int64(6 * time.Hour / time.Millisecond)
	}
	return 0
}

// eval — núcleo DETERMINÍSTICO (testável): dada a leitura atual + estado, decide os
// alertas e atualiza o estado (flips/baseline). Não faz I/O.
func (st *cpState) eval(now time.Time, r cpReading, tickStale, flapWindow time.Duration, flapMax int) []sysAlert {
	var out []sysAlert

	// 1) Tick parado — só após ao menos um tick (idade >= 0).
	if r.tickAge >= 0 && r.tickAge > tickStale {
		out = append(out, sysAlert{"tick-stalled", "Scheduler tick stalled", "critical",
			fmt.Sprintf("The scheduling loop hasn't advanced for %s (limit %s) — jobs may not be getting promoted.",
				r.tickAge.Round(time.Second), tickStale)})
	}

	// 2) DB inacessível.
	if r.dbErr {
		out = append(out, sysAlert{"db-unreachable", "DB unreachable", "critical",
			"The state store (DB) did not answer the ping — the server cannot read or write state."})
	}

	// 3) Leader flapping — transições de liderança na janela.
	if st.hasPrevLeader && r.leaderNow != st.prevLeader {
		st.leaderFlips = append(st.leaderFlips, now)
	}
	st.prevLeader, st.hasPrevLeader = r.leaderNow, true
	cut := now.Add(-flapWindow)
	kept := st.leaderFlips[:0]
	for _, t := range st.leaderFlips {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	st.leaderFlips = kept
	if len(st.leaderFlips) > flapMax {
		out = append(out, sysAlert{"leader-flapping", "Leader flapping", "critical",
			fmt.Sprintf("Leadership changed %d times in %s — HA instability (network/DB/advisory lock).",
				len(st.leaderFlips), flapWindow)})
	}

	// 4) Frota de agentes encolhendo.
	if st.hasPrevAgents && r.agentsOnline < st.prevAgents {
		out = append(out, sysAlert{"agents-drop", "Agent fleet shrank", "warning",
			fmt.Sprintf("Online agents dropped from %d to %d — reduced execution capacity.",
				st.prevAgents, r.agentsOnline)})
	}
	st.prevAgents, st.hasPrevAgents = r.agentsOnline, true

	// 5) Agente travado: UNCERTAIN retém o slot até resolução auditada. Com todos
	//    os slots assim, nada mais é despachado para o agente (crítico); com parte
	//    deles, a capacidade está reduzida sem ninguém ter decidido isso (aviso).
	for _, a := range r.admissions {
		if a.Slots <= 0 || a.Uncertain <= 0 {
			continue
		}
		if a.Uncertain >= a.Slots {
			out = append(out, sysAlert{"agent-blocked:" + a.AgentID, "Agent blocked by unresolved executions", "critical",
				fmt.Sprintf("Agent '%s' dispatches nothing: all %d slots are held by UNCERTAIN executions. Resolve them in the Execution tab of each UNCERTAIN job.",
					a.AgentID, a.Slots)})
			continue
		}
		out = append(out, sysAlert{"agent-uncertain:" + a.AgentID, "Agent slots held by unresolved executions", "warning",
			fmt.Sprintf("Agent '%s' has %d of %d slots held by UNCERTAIN executions until an operator resolves them (Execution tab of each UNCERTAIN job).",
				a.AgentID, a.Uncertain, a.Slots)})
	}

	return out
}
