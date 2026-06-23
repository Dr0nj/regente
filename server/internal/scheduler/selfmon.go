package scheduler

import (
	"context"
	"fmt"
	"log"
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
//
// Só o LÍDER publica (evita N cópias do mesmo alerta no cluster); a exceção é o DB
// inacessível, que é logado em qualquer nó (e a publicação, se houver, é best-effort).
type ControlPlaneMonitor struct {
	alerts       *AlertEngine
	lastTick     func() time.Time
	isLeader     func() bool
	pingDB       func(context.Context) error
	onlineAgents func() int

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
		m.alerts.FireSystem(a.Key, a.Name, a.Severity, a.Message, m.cooldownMs)
	}
}

// cpReading — leitura instantânea da saúde do control plane.
type cpReading struct {
	tickAge      time.Duration // desde o último tick; < 0 se ainda não ticou
	dbErr        bool
	leaderNow    bool
	agentsOnline int
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

// eval — núcleo DETERMINÍSTICO (testável): dada a leitura atual + estado, decide os
// alertas e atualiza o estado (flips/baseline). Não faz I/O.
func (st *cpState) eval(now time.Time, r cpReading, tickStale, flapWindow time.Duration, flapMax int) []sysAlert {
	var out []sysAlert

	// 1) Tick parado — só após ao menos um tick (idade >= 0).
	if r.tickAge >= 0 && r.tickAge > tickStale {
		out = append(out, sysAlert{"tick-stalled", "Scheduler tick parado", "critical",
			fmt.Sprintf("O loop de scheduling não avança há %s (limite %s) — jobs podem não estar sendo promovidos.",
				r.tickAge.Round(time.Second), tickStale)})
	}

	// 2) DB inacessível.
	if r.dbErr {
		out = append(out, sysAlert{"db-unreachable", "DB inacessível", "critical",
			"O state store (DB) não respondeu ao ping — o servidor não consegue ler/gravar estado."})
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
			fmt.Sprintf("A liderança trocou %d vezes em %s — instabilidade de HA (rede/DB/advisory lock).",
				len(st.leaderFlips), flapWindow)})
	}

	// 4) Frota de agentes encolhendo.
	if st.hasPrevAgents && r.agentsOnline < st.prevAgents {
		out = append(out, sysAlert{"agents-drop", "Frota de agentes caiu", "warning",
			fmt.Sprintf("Agentes online caíram de %d para %d — capacidade de execução reduzida.",
				st.prevAgents, r.agentsOnline)})
	}
	st.prevAgents, st.hasPrevAgents = r.agentsOnline, true

	return out
}
