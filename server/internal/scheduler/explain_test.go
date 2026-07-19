package scheduler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

const exDate = "2026-06-24"

// seedWaitingEx insere uma instance WAITING com scheduled_at e def (snapshot) dados.
func seedWaitingEx(t *testing.T, s *Scheduler, id string, schedAt time.Time, def domain.JobDefinition) {
	t.Helper()
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot) VALUES(?,?,?,?,?,?)`,
		id, def.ID, exDate, string(domain.StatusWaiting), schedAt, string(snap),
	); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// seedStatusEx insere uma instance com status arbitrário (p/ upstreams).
func seedStatusEx(t *testing.T, s *Scheduler, id, defID, status string) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at) VALUES(?,?,?,?,?)`,
		id, defID, exDate, status, time.Now(),
	); err != nil {
		t.Fatalf("seed up %s: %v", id, err)
	}
}

func explainOf(t *testing.T, s *Scheduler, id string) Explanation {
	t.Helper()
	ex, err := s.Explain(id)
	if err != nil {
		t.Fatalf("Explain(%s): %v", id, err)
	}
	return ex
}

func hasKind(bs []Blocker, k GateKind) *Blocker {
	for i := range bs {
		if bs[i].Kind == k {
			return &bs[i]
		}
	}
	return nil
}

func TestExplain_Runnable(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "r", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedWaitingEx(t, s, "r-1", time.Now().Add(-time.Hour), def) // horário já passou

	ex := explainOf(t, s, "r-1")
	if !ex.Runnable || len(ex.Blockers) != 0 {
		t.Fatalf("esperava runnable sem bloqueios, veio runnable=%v blockers=%v", ex.Runnable, ex.Blockers)
	}
	// Regressão 2026-07-14: blockers vazio tem que serializar como [] (nunca
	// null) — o front lê exp.blockers.some/.map direto; null derrubava a UI
	// ("Cannot read properties of null") no Explain de um WAITING runnable.
	if ex.Blockers == nil {
		t.Fatal("Blockers não pode ser nil (JSON null quebra o front) — precisa ser []")
	}
	if b, err := json.Marshal(ex); err != nil || !strings.Contains(string(b), `"blockers":[]`) {
		t.Fatalf("esperava \"blockers\":[] no JSON, veio %s (err=%v)", b, err)
	}
}

func TestExplain_Window(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "w", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedWaitingEx(t, s, "w-1", time.Now().Add(2*time.Hour), def) // só abre daqui 2h

	ex := explainOf(t, s, "w-1")
	if ex.Runnable || hasKind(ex.Blockers, GateWindow) == nil {
		t.Fatalf("esperava WAIT_WINDOW, veio runnable=%v blockers=%+v", ex.Runnable, ex.Blockers)
	}
}

// Order Force (force_mode='order') NÃO bypassa a janela de horário: é ordem nova
// fora do agendamento, mas o WindowFrom é gate de runtime. Regressão do report
// "janela 22:58 mas o job entrou 22:57" — Order Force despachava ignorando a janela
// porque o gate ficava sob `if !r.Forced`.
func TestExplain_OrderForceRespectsWindow(t *testing.T) {
	s := newTestScheduler(t)
	now := time.Now()
	future := now.Add(90 * time.Minute) // janela abre daqui 90min
	orderDate := future.Format("2006-01-02")
	def := domain.JobDefinition{ID: "of", JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true, WindowFrom: future.Format("15:04")}}
	snap, _ := json.Marshal(def)
	// Order Force: forced=1, force_mode='order', scheduled_at=now (fora do agendamento).
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, forced, force_mode, definition_snapshot) VALUES(?,?,?,?,?,1,?,?)`,
		"of-1", "of", orderDate, string(domain.StatusWaiting), now, ForceModeOrder, string(snap),
	); err != nil {
		t.Fatalf("seed order-force: %v", err)
	}

	ex := explainOf(t, s, "of-1")
	if ex.Runnable || hasKind(ex.Blockers, GateWindow) == nil {
		t.Fatalf("Order Force com janela futura deveria esperar (WAIT_WINDOW), veio runnable=%v blockers=%+v", ex.Runnable, ex.Blockers)
	}

	// "Run Now" (forced sem force_mode) SEGUE bypassando a janela.
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, forced, definition_snapshot) VALUES(?,?,?,?,?,1,?)`,
		"rn-1", "of", orderDate, string(domain.StatusWaiting), now, string(snap),
	); err != nil {
		t.Fatalf("seed run-now: %v", err)
	}
	if ex := explainOf(t, s, "rn-1"); !ex.Runnable {
		t.Fatalf("Run Now (bypass total) deveria rodar mesmo com janela futura, veio %+v", ex)
	}
}

// Dependência de grafo (snapshot legado com upstream) = condição sintetizada:
// o Explain aponta WAIT_CONDITION da condição A-TO-B enquanto o pai não cria.
func TestExplain_WaitDepBecomesCondition(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "d", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "up", Condition: domain.CondOnSuccess}}}
	seedWaitingEx(t, s, "d-1", time.Now().Add(-time.Hour), def)
	seedStatusEx(t, s, "up-1", "up", string(domain.StatusWaiting)) // upstream ainda rodando

	ex := explainOf(t, s, "d-1")
	b := hasKind(ex.Blockers, GateCondition)
	if b == nil || b.Condition != domain.LinkCondName("up", "d") {
		t.Fatalf("esperava WAIT_CONDITION de '%s', veio %+v", domain.LinkCondName("up", "d"), ex.Blockers)
	}
}

func TestExplain_WaitCondition(t *testing.T) {
	s := newTestScheduler(t)
	s.AttachConditions(NewConditionEngine(s.db))
	def := domain.JobDefinition{ID: "c", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
		ConditionsIn: []string{"FECHAMENTO_OK"}}
	seedWaitingEx(t, s, "c-1", time.Now().Add(-time.Hour), def)

	ex := explainOf(t, s, "c-1")
	b := hasKind(ex.Blockers, GateCondition)
	if b == nil || b.Condition != "FECHAMENTO_OK" {
		t.Fatalf("esperava WAIT_CONDITION 'FECHAMENTO_OK', veio %+v", ex.Blockers)
	}

	// Seta a condition → deixa de bloquear.
	if err := s.conditions.Set("FECHAMENTO_OK", exDate, "test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if ex2 := explainOf(t, s, "c-1"); !ex2.Runnable {
		t.Fatalf("com a condition setada deveria ficar runnable, veio %+v", ex2)
	}
}

func TestExplain_WaitResource(t *testing.T) {
	s := newTestScheduler(t)
	tracker := NewResourceTracker()
	tracker.SetCapacity("db", 1)
	s.AttachResources(tracker)
	def := domain.JobDefinition{ID: "rs", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
		Resources: map[string]int{"db": 2}} // quer 2, cap 1
	seedWaitingEx(t, s, "rs-1", time.Now().Add(-time.Hour), def)

	ex := explainOf(t, s, "rs-1")
	b := hasKind(ex.Blockers, GateResource)
	if b == nil || b.Resource != "db" || b.Capacity != 1 || b.Want != 2 {
		t.Fatalf("esperava WAIT_RESOURCE db want=2 cap=1, veio %+v", ex.Blockers)
	}
	// Explain é READ-ONLY: não pode ter reservado nada no tracker.
	for _, rs := range tracker.Snapshot() {
		if rs.Name == "db" && rs.Used != 0 {
			t.Fatalf("Explain mutou o tracker (used=%d), deveria ser read-only", rs.Used)
		}
	}
}

// Coletar TODOS os bloqueios (modo Explain), não só o primeiro.
func TestExplain_MultipleBlockers(t *testing.T) {
	s := newTestScheduler(t)
	s.AttachConditions(NewConditionEngine(s.db))
	def := domain.JobDefinition{ID: "m", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
		ConditionsIn: []string{"COND_X"}}
	seedWaitingEx(t, s, "m-1", time.Now().Add(2*time.Hour), def) // janela futura + condition faltando

	ex := explainOf(t, s, "m-1")
	if hasKind(ex.Blockers, GateWindow) == nil || hasKind(ex.Blockers, GateCondition) == nil {
		t.Fatalf("esperava WAIT_WINDOW e WAIT_CONDITION juntos, veio %+v", ex.Blockers)
	}
}

func TestExplain_TerminalAndForced(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "t", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}

	// Forçada → runnable bypassando gates.
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, forced, definition_snapshot) VALUES(?,?,?,?,?,1,?)`,
		"f-1", "t", exDate, string(domain.StatusWaiting), time.Now().Add(2*time.Hour), string(snap),
	); err != nil {
		t.Fatalf("seed forced: %v", err)
	}
	if ex := explainOf(t, s, "f-1"); !ex.Runnable {
		t.Fatalf("forced deveria ser runnable (bypass), veio %+v", ex)
	}

	// Estados terminais/ativos: sem blockers, descrição própria.
	for _, st := range []string{string(domain.StatusOK), string(domain.StatusNotOK), string(domain.StatusHeld), string(domain.StatusRunning)} {
		id := "term-" + st
		seedStatusEx(t, s, id, "t", st)
		ex := explainOf(t, s, id)
		if ex.Runnable || len(ex.Blockers) != 0 || ex.Summary == "" {
			t.Fatalf("estado %s: esperava não-runnable, sem blockers, com summary; veio %+v", st, ex)
		}
	}
}

func TestExplain_NotFound(t *testing.T) {
	s := newTestScheduler(t)
	if _, err := s.Explain("nao-existe"); err == nil {
		t.Fatal("Explain de instance inexistente deveria retornar erro")
	}
}
