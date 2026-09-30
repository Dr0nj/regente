package scheduler

import (
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/storage"
	"sync"
	"testing"
	"time"
)

func i07Defs(n int) []domain.JobDefinition {
	defs := make([]domain.JobDefinition, n)
	for i := range defs {
		defs[i] = domain.JobDefinition{ID: fmt.Sprintf("job-%05d", i), Team: "test", Params: map[string]interface{}{"command": "original"}, JobType: "DUMMY", Schedule: domain.Schedule{Enabled: true}, Confirm: true}
	}
	return defs
}
func TestI07CheckpointRecovery(t *testing.T) {
	s := newTestScheduler(t)
	s.defs = i07Defs(dailyBatchChunk + 1)
	date := "2026-09-30"
	if _, err := s.db.Exec(`CREATE TRIGGER daily_fault BEFORE INSERT ON instances WHEN NEW.id='job-05000-2026-09-30' BEGIN SELECT RAISE(ABORT,'synthetic second chunk failure'); END`); err != nil {
		t.Fatal(err)
	}
	run, n, err := s.MaterializeDaily(date)
	if err == nil || n != dailyBatchChunk || run.State != "failed" || run.Checkpoint != dailyBatchChunk || run.Inserted != dailyBatchChunk || run.FinishedAt != nil {
		t.Fatalf("falha parcial n=%d run=%+v err=%v", n, run, err)
	}
	ex, err := s.Explain("job-00000-" + date)
	if err != nil || ex.Runnable || len(ex.Blockers) != 1 || ex.Blockers[0].Kind != GateConfiguration {
		t.Fatalf("daily parcial executável %+v %v", ex, err)
	}
	if _, err := s.db.Exec("UPDATE instances SET forced=1 WHERE id='job-00000-2026-09-30'"); err != nil {
		t.Fatal(err)
	}
	s.tickOnce()
	var status string
	s.db.QueryRow("SELECT status FROM instances WHERE id='job-00000-2026-09-30'").Scan(&status)
	if status != "WAITING" {
		t.Fatal("Run Now ignorou daily incompleta")
	}
	if _, err := s.db.Exec("DROP TRIGGER daily_fault"); err != nil {
		t.Fatal(err)
	}
	// Novo scheduler sem definitions prova a fonte durável; settings posteriores não alteram plano.
	var seq int
	var name, path string
	if err := s.db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	s.Stop()
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(db.SQLite, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	next := New(storage.NewFileStore(t.TempDir(), false), reopened, hub.New(), time.Hour)
	t.Cleanup(next.Stop)
	next.defs = []domain.JobDefinition{{ID: "published-later", Params: map[string]interface{}{"command": "new"}}}
	next.settings = Settings{Timezone: "Asia/Tokyo", DailyAt: "06:00"}
	run, n, err = next.MaterializeDaily(date)
	if err != nil || n != 1 || run.State != "completed" || run.Checkpoint != 5001 || run.Inserted != 5001 || run.FinishedAt == nil {
		t.Fatalf("retomada %+v %d %v", run, n, err)
	}
	var count int
	next.db.QueryRow("SELECT COUNT(*) FROM instance_events WHERE kind='ordered'").Scan(&count)
	if count != 5001 {
		t.Fatalf("eventos duplicados/perdidos: %d", count)
	}
	for _, id := range []string{"job-00000-", "job-05000-"} {
		var snap string
		if e := next.db.QueryRow("SELECT definition_snapshot FROM instances WHERE id=?", id+date).Scan(&snap); e != nil {
			t.Fatal(e)
		}
		var d domain.JobDefinition
		json.Unmarshal([]byte(snap), &d)
		if d.Params["command"] != "original" || d.BusinessTime.Timezone != "UTC" {
			t.Fatal("misturou fonte/config após restart")
		}
	}
	if _, n, e := next.MaterializeDaily(date); e != nil || n != 0 {
		t.Fatal("retomada não idempotente", n, e)
	}
	// Delete legítimo depois do checkpoint não ressuscita por reexecução da daily.
	next.db.Exec("DELETE FROM instances WHERE id='job-00000-2026-09-30'")
	if _, n, e := next.MaterializeDaily(date); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}
func TestI07ConcurrentDaily(t *testing.T) {
	s := newTestScheduler(t)
	s.defs = i07Defs(25)
	other := New(s.store, s.db, hub.New(), time.Hour)
	other.defs = i07Defs(25)
	t.Cleanup(other.Stop)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, node := range []*Scheduler{s, other} {
		wg.Add(1)
		go func(node *Scheduler) {
			defer wg.Done()
			<-start
			_, _, e := node.MaterializeDaily("2026-09-30")
			results <- e
		}(node)
	}
	close(start)
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM instances").Scan(&count)
	if count != 25 {
		t.Fatal("duplicou", count)
	}
	run, e := s.DailyRun("2026-09-30")
	if e != nil || run.State != "completed" || run.Inserted != 25 {
		t.Fatal(run, e)
	}
}
func TestI07CorruptSnapshotAndPlan(t *testing.T) {
	s := newTestScheduler(t)
	s.defs = i07Defs(1)
	date := "2026-09-30"
	s.SetClock(businessclock.Func(func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }))
	s.defs[0].ConditionsOutAdd = []string{"frozen-out"}
	s.conditions = NewConditionEngine(s.db)
	s.RunDaily(date)
	s.defs[0].ConditionsOutAdd = []string{"live-out"}
	for _, raw := range []string{"{broken", "null", "{}", `{"id":"different"}`, `{"id":"job-00000","jobType":"COMMAND","actionConfig":{"command":"changed"},"conditionsOutAdd":["tampered-out"]}`, ""} {
		s.db.Exec("UPDATE instances SET definition_snapshot=?,forced=1 WHERE id='job-00000-2026-09-30'", raw)
		s.tickOnce()
		s.applyConditionsOut("job-00000-"+date, "test")
		for _, name := range []string{"frozen-out", "live-out", "tampered-out"} {
			if s.conditions.Has(name, date) {
				t.Fatalf("snapshot corrompido aplicou %s", name)
			}
		}
		if _, _, _, ok := s.instanceContext("job-00000-" + date); ok {
			t.Fatal("On-Do aceitou snapshot corrompido")
		}
		ex, e := s.Explain("job-00000-" + date)
		if e != nil || ex.Runnable || len(ex.Blockers) != 1 || ex.Blockers[0].Kind != GateConfiguration {
			t.Fatalf("snapshot %s: %+v %v", raw, ex, e)
		}
	}
	s.db.Exec("UPDATE daily_runs SET state='failed',finished_at=NULL,plan_json='[]' WHERE order_date=?", date)
	run, n, e := s.MaterializeDaily(date)
	if e == nil || n != 0 || run.State != "failed" {
		t.Fatal("plano corrompido não bloqueou", run, n, e)
	}
}
func TestI07AutomaticRecoveryBeforeRollover(t *testing.T) {
	s := newTestScheduler(t)
	s.defs = i07Defs(1)
	s.SetClock(businessclock.Func(func() time.Time { return time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC) }))
	s.settings.DailyAt = "06:00"
	if e := s.freezeDailyPlan("2026-09-30"); e != nil {
		t.Fatal(e)
	}
	s.defs = nil
	s.autoDailyIfDue()
	run, e := s.DailyRun("2026-09-30")
	if e != nil || run.State != "completed" {
		t.Fatal("não retomou antes do rollover", run, e)
	}
}

func TestI07CarryFailureIsAtomic(t *testing.T) {
	s := newTestScheduler(t)
	s.defs = i07Defs(2)
	s.RunDaily("2026-09-29")
	s.db.Exec("UPDATE instances SET status='HELD'")
	if _, e := s.db.Exec(`CREATE TRIGGER carry_fault BEFORE INSERT ON instance_events WHEN NEW.kind='carried' AND NEW.instance_id='job-00001-2026-09-29' BEGIN SELECT RAISE(ABORT,'synthetic carry event failure'); END`); e != nil {
		t.Fatal(e)
	}
	run, n, e := s.MaterializeDaily("2026-09-30")
	if e == nil || n != 0 || run.State != "failed" || run.Checkpoint != 0 {
		t.Fatal(run, n, e)
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM instances WHERE order_date='2026-09-29'").Scan(&count)
	if count != 2 {
		t.Fatal("carry parcial", count)
	}
	s.db.QueryRow("SELECT COUNT(*) FROM instance_events WHERE kind='carried'").Scan(&count)
	if count != 0 {
		t.Fatal("evento carry parcial")
	}
	s.db.Exec("DROP TRIGGER carry_fault")
	run, n, e = s.MaterializeDaily("2026-09-30")
	if e != nil || n != 2 || run.Carried != 2 || run.State != "completed" {
		t.Fatal(run, n, e)
	}
}
