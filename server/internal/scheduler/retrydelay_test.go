package scheduler

// D-1 — retry com delay durável (retryDelayMin) + long-running human-in-the-loop.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// REGRA: com retryDelayMin>0, a falha NÃO re-dispatcha na hora: a instance
// volta pra WAITING com scheduled_at = agora + delay, persistido no DB — o
// agendamento sobrevive a restart (nenhuma goroutine dormindo carrega o prazo).
func TestMaybeRetry_DelayIsScheduledNotSlept(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{
		ID: "d", Label: "Delayed", JobType: "COMMAND",
		Retries: 2, RetryDelayMin: 60, // 1h — se fosse sleep, o teste nunca acabaria
	}
	snap, _ := json.Marshal(def)
	id := "d-2026-07-07"
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot, attempts)
		 VALUES(?,?,?,?,?,?,1)`,
		id, "d", "2026-07-07", string(domain.StatusRunning), time.Now(), string(snap),
	); err != nil {
		t.Fatal(err)
	}

	before := time.Now()
	s.FinishInstance(id, domain.StatusNotOK, 1, "boom")

	var status string
	var attempts int
	var schedAt time.Time
	if err := s.db.QueryRow(
		`SELECT status, attempts, scheduled_at FROM instances WHERE id=?`, id,
	).Scan(&status, &attempts, &schedAt); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.StatusWaiting) || attempts != 2 {
		t.Fatalf("esperava WAITING attempts=2, veio %s %d", status, attempts)
	}
	// scheduled_at ~1h no futuro (tolerância de 5min pra clock/UTC-vs-local do driver)
	wantMin := before.Add(55 * time.Minute)
	wantMax := before.Add(65 * time.Minute)
	if schedAt.Before(wantMin) || schedAt.After(wantMax) {
		t.Fatalf("scheduled_at deveria ser ~+60min, veio %s (agora=%s)", schedAt, before)
	}
}

// REGRA: sem retryDelayMin o comportamento clássico segue: retry re-dispatcha
// com backoff curto e o job termina OK dentro do orçamento (DemoMode mocka).
func TestMaybeRetry_NoDelayKeepsClassicBackoff(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "c", Label: "Classic", JobType: "COMMAND", Retries: 1}
	snap, _ := json.Marshal(def)
	id := "c-2026-07-07"
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot, attempts)
		 VALUES(?,?,?,?,?,?,1)`,
		id, "c", "2026-07-07", string(domain.StatusRunning), time.Now(), string(snap),
	); err != nil {
		t.Fatal(err)
	}
	s.FinishInstance(id, domain.StatusNotOK, 1, "boom")

	var status string
	var schedAt time.Time
	_ = s.db.QueryRow(`SELECT status, scheduled_at FROM instances WHERE id=?`, id).Scan(&status, &schedAt)
	if status != string(domain.StatusWaiting) {
		t.Fatalf("retry clássico deveria voltar pra WAITING, veio %s", status)
	}
	// clássico NÃO empurra scheduled_at pro futuro distante
	if schedAt.After(time.Now().Add(5 * time.Minute)) {
		t.Fatalf("retry clássico não agenda pro futuro, scheduled_at=%s", schedAt)
	}
}

// REGRA (integração das duas pontas do D-1): um retry agendado pra depois da
// virada da daily SOBREVIVE ao carry-over — chega no novo dia como WAITING com
// o MESMO scheduled_at (o prazo de "retry após 3 dias" não é resetado).
func TestRetryPending_SurvivesDailyTurnover(t *testing.T) {
	s := newTestScheduler(t)
	// keepActive=3 cobre o prazo do retry: desde 2026-07-16 a sobrevivência é em
	// DIAS-CALENDÁRIO desde a última execução (baseline 1 sem keepActive) — um
	// "retry após 3 dias" declara keepActive>=3 pra atravessar as viradas.
	def := domain.JobDefinition{
		ID: "r3d", JobType: "COMMAND", Retries: 3, RetryDelayMin: 3 * 24 * 60,
		Schedule: domain.Schedule{KeepActive: 3},
	}
	snap, _ := json.Marshal(def)
	future := time.Now().Add(72 * time.Hour)
	if _, err := s.db.Exec(
		// attempts=2 + started_at preenchido: já RODOU e falhou 1x, retry agendado
		// (o handleRetry preserva o started_at da tentativa falha — é o que
		// distingue retry em tratamento de um rerun de operador, que o zera).
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot, attempts, started_at)
		 VALUES(?,?,?,?,?,?,2,?)`,
		"r3d-i", "r3d", "2026-07-07", string(domain.StatusWaiting), future, string(snap), time.Now().Add(-time.Hour),
	); err != nil {
		t.Fatal(err)
	}

	s.RunDaily("2026-07-08")

	var od, status string
	var schedAt time.Time
	if err := s.db.QueryRow(
		`SELECT order_date, status, scheduled_at FROM instances WHERE id='r3d-i'`,
	).Scan(&od, &status, &schedAt); err != nil {
		t.Fatal(err)
	}
	if od != "2026-07-08" || status != string(domain.StatusWaiting) {
		t.Fatalf("retry agendado deveria carregar pra nova daily como WAITING, veio %s %s", od, status)
	}
	if diff := schedAt.Sub(future); diff < -time.Minute || diff > time.Minute {
		t.Fatalf("scheduled_at do retry não podia mudar na virada, drift=%s", diff)
	}
}
