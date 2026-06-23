package scheduler

import (
	"testing"
	"time"
)

const (
	testStale = 90 * time.Second
	testFlapW = 5 * time.Minute
	testFlapN = 5
)

func keys(alerts []sysAlert) map[string]bool {
	m := map[string]bool{}
	for _, a := range alerts {
		m[a.Key] = true
	}
	return m
}

// R7 — tick parado dispara só acima do limiar; nunca antes do 1º tick (idade < 0).
func TestSelfmon_TickStalled(t *testing.T) {
	now := time.Now()
	var st cpState

	if keys(st.eval(now, cpReading{tickAge: -1}, testStale, testFlapW, testFlapN))["tick-stalled"] {
		t.Fatal("idade < 0 (nunca ticou) não deveria alertar tick parado")
	}
	if keys(st.eval(now, cpReading{tickAge: 30 * time.Second}, testStale, testFlapW, testFlapN))["tick-stalled"] {
		t.Fatal("tick recente não deveria alertar")
	}
	if !keys(st.eval(now, cpReading{tickAge: 2 * time.Minute}, testStale, testFlapW, testFlapN))["tick-stalled"] {
		t.Fatal("tick acima do limiar deveria alertar")
	}
}

// R7 — DB inacessível dispara o alerta crítico.
func TestSelfmon_DBUnreachable(t *testing.T) {
	var st cpState
	if !keys(st.eval(time.Now(), cpReading{tickAge: 1 * time.Second, dbErr: true}, testStale, testFlapW, testFlapN))["db-unreachable"] {
		t.Fatal("dbErr deveria alertar db-unreachable")
	}
}

// R7 — leader flapping: só alerta quando as transições passam de flapMax na janela.
func TestSelfmon_LeaderFlapping(t *testing.T) {
	var st cpState
	base := time.Now()
	// Alterna a liderança a cada 10s; cada chamada com leaderNow diferente conta 1 flip.
	fired := false
	leader := true
	for i := 0; i < 9; i++ {
		now := base.Add(time.Duration(i) * 10 * time.Second)
		alerts := st.eval(now, cpReading{tickAge: time.Second, leaderNow: leader}, testStale, testFlapW, testFlapN)
		if keys(alerts)["leader-flapping"] {
			fired = true
		}
		leader = !leader
	}
	if !fired {
		t.Fatalf("esperava leader-flapping após >%d trocas na janela", testFlapN)
	}
}

// R7 — flips fora da janela são podados: trocas espaçadas não alertam.
func TestSelfmon_LeaderFlapping_WindowPrune(t *testing.T) {
	var st cpState
	base := time.Now()
	leader := true
	for i := 0; i < 12; i++ {
		now := base.Add(time.Duration(i) * 2 * time.Minute) // 2min entre trocas → só ~2-3 na janela de 5min
		alerts := st.eval(now, cpReading{tickAge: time.Second, leaderNow: leader}, testStale, testFlapW, testFlapN)
		if keys(alerts)["leader-flapping"] {
			t.Fatal("trocas espaçadas (fora da janela) não deveriam alertar flapping")
		}
		leader = !leader
	}
}

// R7 — frota de agentes: queda alerta; estável/crescimento não.
func TestSelfmon_AgentsDrop(t *testing.T) {
	var st cpState
	now := time.Now()
	// baseline 3 agentes (1ª leitura não alerta).
	if keys(st.eval(now, cpReading{tickAge: time.Second, agentsOnline: 3}, testStale, testFlapW, testFlapN))["agents-drop"] {
		t.Fatal("1ª leitura (sem baseline) não deveria alertar")
	}
	// cresce 3 → 4: sem alerta.
	if keys(st.eval(now, cpReading{tickAge: time.Second, agentsOnline: 4}, testStale, testFlapW, testFlapN))["agents-drop"] {
		t.Fatal("crescimento não deveria alertar")
	}
	// cai 4 → 1: alerta.
	if !keys(st.eval(now, cpReading{tickAge: time.Second, agentsOnline: 1}, testStale, testFlapW, testFlapN))["agents-drop"] {
		t.Fatal("queda de agentes deveria alertar")
	}
	// estável 1 → 1: sem alerta.
	if keys(st.eval(now, cpReading{tickAge: time.Second, agentsOnline: 1}, testStale, testFlapW, testFlapN))["agents-drop"] {
		t.Fatal("estável não deveria alertar")
	}
}

// R7 — tudo saudável: nenhum alerta.
func TestSelfmon_Healthy(t *testing.T) {
	var st cpState
	now := time.Now()
	_ = st.eval(now, cpReading{tickAge: time.Second, leaderNow: true, agentsOnline: 2}, testStale, testFlapW, testFlapN)
	got := st.eval(now.Add(time.Second), cpReading{tickAge: time.Second, leaderNow: true, agentsOnline: 2}, testStale, testFlapW, testFlapN)
	if len(got) != 0 {
		t.Fatalf("estado saudável não deveria gerar alertas, veio %+v", got)
	}
}
