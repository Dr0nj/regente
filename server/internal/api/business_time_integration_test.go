package api

import (
	"encoding/json"
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

func TestI06BusinessTimeIntegration(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			h := hub.New()
			store := storage.NewFileStore(t.TempDir(), false)
			def := domain.JobDefinition{ID: "clock", Label: "Clock", Team: "time", JobType: "COMMAND", Confirm: true, Schedule: domain.Schedule{Enabled: true, RunAt: "02:00", WindowTo: "03:00"}, SLA: &domain.SLASpec{DeadlineHM: "02:30", ExpectedDurationMin: 5}}
			if err := store.Save(def); err != nil {
				t.Fatal(err)
			}
			sched := scheduler.New(store, d, h, time.Hour)
			t.Cleanup(sched.Stop)
			now := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC)
			sched.SetClock(businessclock.Func(func() time.Time { return now }))
			sched.ReloadDefs()
			srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Store: store, Scheduler: sched, Token: "test-token"}))
			defer srv.Close()
			machineRequest(t, srv, "PUT", "/api/settings", "test-token", map[string]string{"daily_timezone": "America/Sao_Paulo", "daily_at": "06:00"}, 200)
			raw := machineRequest(t, srv, "POST", "/api/daily/run", "test-token", nil, 200)
			var daily struct {
				OrderDate string
				Created   int
			}
			if err := json.Unmarshal(raw, &daily); err != nil {
				t.Fatal(err)
			}
			if daily.OrderDate != "2026-09-29" || daily.Created != 1 {
				t.Fatalf("daily %s", raw)
			}
			raw = machineRequest(t, srv, "GET", "/api/instances?date=2026-09-29", "test-token", nil, 200)
			var instances []instanceRow
			if err := json.Unmarshal(raw, &instances); err != nil {
				t.Fatal(err)
			}
			if len(instances) != 1 || instances[0].BusinessTime == nil || instances[0].BusinessTime.Timezone != "America/Sao_Paulo" || !instances[0].ScheduledAt.Equal(now) {
				t.Fatalf("instances %s", raw)
			}
			raw = machineRequest(t, srv, "GET", "/api/forecast?date=2026-09-29", "test-token", nil, 200)
			var forecast scheduler.ForecastReport
			_ = json.Unmarshal(raw, &forecast)
			if len(forecast.Jobs) != 1 || !forecast.Jobs[0].StartAt.Equal(now) || forecast.BusinessTime.DailyAt != "06:00" {
				t.Fatalf("forecast %s", raw)
			}
			raw = machineRequest(t, srv, "POST", "/api/whatif", "test-token", map[string]any{"date": "2026-09-29", "changes": []any{}}, 200)
			var what scheduler.WhatIfReport
			_ = json.Unmarshal(raw, &what)
			if len(what.Rows) != 1 || what.Rows[0].BaseStart == nil || !what.Rows[0].BaseStart.Equal(now) {
				t.Fatalf("what-if %s", raw)
			}
			// SLA usa snapshot/ODAT e instante fornecido, inclusive após trocar settings.
			machineRequest(t, srv, "PUT", "/api/settings", "test-token", map[string]string{"daily_timezone": "Asia/Tokyo", "daily_at": "00:00"}, 200)
			if _, err := d.Exec("UPDATE instances SET status='RUNNING',started_at=? WHERE id='clock-2026-09-29'", now); err != nil {
				t.Fatal(err)
			}
			sla := scheduler.NewSLAEngine(d, nil)
			later := now.Add(31 * time.Minute)
			sla.Evaluate(map[string]domain.JobDefinition{"clock": def}, later)
			var detected time.Time
			if err := d.QueryRow("SELECT detected_at FROM sla_breaches WHERE instance_id='clock-2026-09-29' AND kind='deadline'").Scan(&detected); err != nil {
				t.Fatal(err)
			}
			if !detected.Equal(later) {
				t.Fatalf("SLA usou relógio real: %v", detected)
			}
			raw = machineRequest(t, srv, "GET", "/api/instances?date=2026-09-29", "test-token", nil, 200)
			_ = json.Unmarshal(raw, &instances)
			if instances[0].BusinessTime.Timezone != "America/Sao_Paulo" || !instances[0].ScheduledAt.Equal(now) {
				t.Fatalf("ordem reinterpretada: %s", raw)
			}
		})
	}
}
