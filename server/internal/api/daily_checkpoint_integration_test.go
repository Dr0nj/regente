package api

import (
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"net/http/httptest"
	"testing"
	"time"
)

func TestI07DailyRecoveryIntegration(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			store := storage.NewFileStore(t.TempDir(), false)
			for i := 0; i < 5001; i++ {
				if e := store.Save(domain.JobDefinition{ID: fmt.Sprintf("job-%05d", i), Team: "test", Params: map[string]interface{}{"command": "original"}, JobType: "COMMAND", Confirm: true, Schedule: domain.Schedule{Enabled: true}}); e != nil {
					t.Fatal(e)
				}
			}
			h := hub.New()
			s := scheduler.New(store, d, h, time.Hour)
			t.Cleanup(s.Stop)
			s.ReloadDefs()
			clock := businessclock.Func(func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) })
			s.SetClock(clock)
			srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Store: store, Scheduler: s, Token: "test-token"}))
			defer srv.Close()
			trigger := `CREATE TRIGGER daily_fault BEFORE INSERT ON instances WHEN NEW.id='job-05000-2026-09-30' BEGIN SELECT RAISE(ABORT,'synthetic second chunk failure'); END`
			if dialect == db.Postgres {
				if _, e := d.Exec(`CREATE FUNCTION daily_fault_fn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='job-05000-2026-09-30' THEN RAISE EXCEPTION 'synthetic second chunk failure'; END IF; RETURN NEW; END $$`); e != nil {
					t.Fatal(e)
				}
				trigger = `CREATE TRIGGER daily_fault BEFORE INSERT ON instances FOR EACH ROW EXECUTE FUNCTION daily_fault_fn()`
			}
			if _, e := d.Exec(trigger); e != nil {
				t.Fatal(e)
			}
			raw := machineRequest(t, srv, "POST", "/api/daily/run", "test-token", nil, 409)
			var response struct {
				Created int
				Run     scheduler.DailyRun
			}
			if e := json.Unmarshal(raw, &response); e != nil {
				t.Fatal(e)
			}
			if response.Created != 5000 || response.Run.State != "failed" || response.Run.Expected != 5001 || response.Run.Checkpoint != 5000 {
				t.Fatalf("falha parcial %s", raw)
			}
			raw = machineRequest(t, srv, "GET", "/api/daily/status", "test-token", nil, 200)
			var status struct {
				Pending     *scheduler.DailyRun
				LastRunDate string
			}
			json.Unmarshal(raw, &status)
			if status.Pending == nil || !status.Pending.CanResume || status.LastRunDate != "" {
				t.Fatalf("estado incompleto oculto %s", raw)
			}
			drop := "DROP TRIGGER daily_fault"
			if dialect == db.Postgres {
				drop += " ON instances"
			}
			if _, e := d.Exec(drop); e != nil {
				t.Fatal(e)
			}
			// Outro nó, definitions novas e settings diferentes não mudam o plano congelado.
			empty := storage.NewFileStore(t.TempDir(), false)
			next := scheduler.New(empty, d, h, time.Hour)
			t.Cleanup(next.Stop)
			next.SetClock(clock)
			resumed := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Store: empty, Scheduler: next, Token: "test-token"}))
			defer resumed.Close()
			machineRequest(t, resumed, "PUT", "/api/settings", "test-token", map[string]string{"daily_timezone": "Asia/Tokyo", "daily_at": "06:00"}, 200)
			raw = machineRequest(t, resumed, "POST", "/api/daily/resume", "test-token", map[string]string{"orderDate": "2026-09-30"}, 200)
			json.Unmarshal(raw, &response)
			if response.Created != 1 || response.Run.State != "completed" || response.Run.Inserted != 5001 {
				t.Fatalf("retomada %s", raw)
			}
			for _, id := range []string{"job-00000-2026-09-30", "job-05000-2026-09-30"} {
				var snap string
				d.QueryRow("SELECT definition_snapshot FROM instances WHERE id=?", id).Scan(&snap)
				var def domain.JobDefinition
				json.Unmarshal([]byte(snap), &def)
				if def.Params["command"] != "original" || def.BusinessTime.Timezone != "UTC" {
					t.Fatal("snapshot reinterpretado")
				}
			}
			machineRequest(t, resumed, "POST", "/api/daily/resume", "test-token", map[string]string{"orderDate": "2026-09-30"}, 200)
			d.Exec("UPDATE instances SET definition_snapshot='{broken',forced=1 WHERE id='job-00000-2026-09-30'")
			raw = machineRequest(t, resumed, "GET", "/api/instances/job-00000-2026-09-30/explain", "test-token", nil, 200)
			var ex scheduler.Explanation
			json.Unmarshal(raw, &ex)
			if ex.Runnable || len(ex.Blockers) != 1 || ex.Blockers[0].Kind != scheduler.GateConfiguration {
				t.Fatalf("corrupção sem diagnóstico %s", raw)
			}
			raw = machineRequest(t, resumed, "GET", "/api/instances/job-00000-2026-09-30", "test-token", nil, 200)
			var detail instanceDetail
			if err := json.Unmarshal(raw, &detail); err != nil || detail.SnapshotError == "" || len(detail.SnapshotDef) != 0 {
				t.Fatalf("detalhe de corrupção %s: %v", raw, err)
			}
			var count int
			d.QueryRow("SELECT COUNT(*) FROM instance_events WHERE kind='ordered'").Scan(&count)
			if count != 5001 {
				t.Fatal("evento/ordem duplicado", count)
			}
		})
	}
}
