package scheduler

// Slow Execution — média histórica +50% (2026-07-17). Trava a semântica nova
// da rule-slow: lento = passou da média das execuções OK ANTERIORES na folga
// configurada (percentOver, default 50); primeira execução NUNCA alerta; o
// disparo sai DURANTE a run (varredura do tick) e não repete no terminal.

import (
	"fmt"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func TestAlertEngine_SlowVsAverage(t *testing.T) {
	eng := NewAlertEngine(newTestDB(t), nil)
	eng.SeedDefaults()

	base := AlertContext{WorkflowName: "Job Slow", Status: string(domain.StatusOK)}

	// 1ª execução (sem histórico): 10min, mas sem régua → nada.
	first := base
	first.WorkflowID = "job-first"
	first.DurationMs = 600_000
	first.HistoryRuns = 0
	eng.Evaluate(first)
	if evs, _ := eng.ListEvents(50); len(evs) != 0 {
		t.Fatalf("primeira execução não pode alertar lentidão, veio %d", len(evs))
	}

	// Média 10min, rodou 14min (+40%): dentro da folga de 50% → nada.
	okish := base
	okish.WorkflowID = "job-okish"
	okish.DurationMs = 840_000
	okish.AvgDurationMs = 600_000
	okish.HistoryRuns = 5
	eng.Evaluate(okish)
	if evs, _ := eng.ListEvents(50); len(evs) != 0 {
		t.Fatalf("+40%% sobre a média (folga 50%%) não deveria alertar, veio %d", len(evs))
	}

	// Média 10min, rodou 16min (+60%): estourou → dispara a rule-slow.
	slow := base
	slow.WorkflowID = "job-slow"
	slow.DurationMs = 960_000
	slow.AvgDurationMs = 600_000
	slow.HistoryRuns = 5
	eng.Evaluate(slow)
	evs, _ := eng.ListEvents(50)
	if len(evs) != 1 || evs[0].RuleID != "rule-slow" {
		t.Fatalf("+60%% deveria disparar rule-slow, veio %+v", evs)
	}

	// SlowFired (já alertou durante a run) → o terminal não repete.
	dedup := slow
	dedup.WorkflowID = "job-dedup"
	dedup.SlowFired = true
	eng.Evaluate(dedup)
	if evs, _ := eng.ListEvents(50); len(evs) != 1 {
		t.Fatalf("run que já alertou durante a execução não repete no terminal, veio %d", len(evs))
	}
}

func TestAlertEngine_FireSlowRulesRunning(t *testing.T) {
	eng := NewAlertEngine(newTestDB(t), nil)
	eng.SeedDefaults()
	rules := eng.SlowRules()
	if len(rules) != 1 {
		t.Fatalf("esperava 1 regra slow_vs_average habilitada, veio %d", len(rules))
	}
	ctx := AlertContext{
		WorkflowID: "job-run", WorkflowName: "Job Run", Running: true,
		DurationMs: 960_000, AvgDurationMs: 600_000, HistoryRuns: 3,
	}
	if !eng.FireSlowRules(rules, ctx) {
		t.Fatal("RUNNING 60% acima da média deveria disparar")
	}
	evs, _ := eng.ListEvents(50)
	if len(evs) != 1 || evs[0].RuleID != "rule-slow" {
		t.Fatalf("evento inesperado: %+v", evs)
	}
}

// Migração de boot: rule-slow com o default ANTIGO (teto fixo de 30s) vira a
// régua por média; condição CUSTOMIZADA pelo usuário fica intocada.
func TestAlertEngine_SeedMigratesOldSlowDefault(t *testing.T) {
	database := newTestDB(t)
	eng := NewAlertEngine(database, nil)
	for _, r := range defaultRules {
		condJSON := r.ConditionJSON
		if r.ID == "rule-slow" {
			condJSON = oldSlowDefaultJSON
		}
		if _, err := database.Exec(
			`INSERT INTO alert_rules(id, name, enabled, workflow_pattern, condition_json, severity, channels, cooldown_ms)
			 VALUES(?,?,?,?,?,?,?,?)`,
			r.ID, r.Name, boolToInt(r.Enabled), r.WorkflowPattern, condJSON, r.Severity, r.Channels, r.CooldownMs,
		); err != nil {
			t.Fatal(err)
		}
	}
	eng.SeedDefaults()
	rules, _ := eng.ListRules()
	for _, r := range rules {
		if r.ID == "rule-slow" && r.ConditionJSON != slowDefaultJSON {
			t.Fatalf("rule-slow deveria ter migrado pro default novo, veio %s", r.ConditionJSON)
		}
	}

	db2 := newTestDB(t)
	eng2 := NewAlertEngine(db2, nil)
	custom := `{"type":"duration_exceeded","thresholdMs":99000}`
	if _, err := db2.Exec(
		`INSERT INTO alert_rules(id, name, enabled, workflow_pattern, condition_json, severity, channels, cooldown_ms)
		 VALUES('rule-slow','Slow Execution',1,'*',?,'warning','toast',300000)`, custom); err != nil {
		t.Fatal(err)
	}
	eng2.SeedDefaults()
	rules2, _ := eng2.ListRules()
	for _, r := range rules2 {
		if r.ID == "rule-slow" && r.ConditionJSON != custom {
			t.Fatalf("regra customizada não podia ser migrada, veio %s", r.ConditionJSON)
		}
	}
}

// Fim a fim no scheduler: histórico OK de ~10s, uma RUNNING há 16s → a
// varredura do tick dispara UMA vez (dedupe por run nos ticks seguintes).
func TestScheduler_EvaluateSlowRunning(t *testing.T) {
	s := newTestScheduler(t)
	eng := NewAlertEngine(s.db, nil)
	eng.SeedDefaults()
	s.AttachAlerts(eng)

	now := time.Now().UTC()
	// histórico: 3 execuções OK de 10s cada (a régua)
	for i := 0; i < 3; i++ {
		st := now.Add(time.Duration(-(i+2)) * time.Hour)
		fi := st.Add(10 * time.Second)
		if _, err := s.db.Exec(
			`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at, finished_at)
			 VALUES(?, 'def-lento', ?, 'OK', ?, ?, ?)`,
			fmt.Sprintf("hist-%d", i), now.Format("2006-01-02"), st, st, fi,
		); err != nil {
			t.Fatal(err)
		}
	}
	// RUNNING há 16s (média 10s + 50% = 15s → estourou)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at)
		 VALUES('run-1', 'def-lento', ?, 'RUNNING', ?, ?)`,
		now.Format("2006-01-02"), now.Add(-16*time.Second), now.Add(-16*time.Second),
	); err != nil {
		t.Fatal(err)
	}

	s.evaluateSlowRunning(now)
	evs, _ := eng.ListEvents(50)
	if len(evs) != 1 || evs[0].RuleID != "rule-slow" {
		t.Fatalf("RUNNING 60%% acima da média deveria ter 1 alerta rule-slow, veio %+v", evs)
	}
	// tick seguinte: mesma run NÃO re-alerta (dedupe por instance)
	s.evaluateSlowRunning(now.Add(2 * time.Second))
	if evs, _ := eng.ListEvents(50); len(evs) != 1 {
		t.Fatalf("a mesma run não pode re-alertar no tick seguinte, veio %d", len(evs))
	}
}

// Primeira execução fim a fim: RUNNING longa SEM histórico OK → silêncio.
func TestScheduler_SlowRunningFirstExecutionQuiet(t *testing.T) {
	s := newTestScheduler(t)
	eng := NewAlertEngine(s.db, nil)
	eng.SeedDefaults()
	s.AttachAlerts(eng)

	now := time.Now().UTC()
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at)
		 VALUES('run-first', 'def-novato', ?, 'RUNNING', ?, ?)`,
		now.Format("2006-01-02"), now.Add(-1*time.Hour), now.Add(-1*time.Hour),
	); err != nil {
		t.Fatal(err)
	}
	s.evaluateSlowRunning(now)
	if evs, _ := eng.ListEvents(50); len(evs) != 0 {
		t.Fatalf("primeira execução (sem histórico) não alerta mesmo rodando 1h, veio %d", len(evs))
	}
}
