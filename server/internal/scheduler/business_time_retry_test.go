package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/domain"
	"sync/atomic"
	"testing"
	"time"
)

func TestI06LongRetryCarryAndClock(t *testing.T) {
	s := newTestScheduler(t)
	setSetting(t, s, "daily_timezone", "America/New_York")
	setSetting(t, s, "daily_at", "06:00")
	start := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	var stamp atomic.Int64
	stamp.Store(start.UnixNano())
	s.SetClock(businessclock.Func(func() time.Time { return time.Unix(0, stamp.Load()) }))
	c := s.BusinessCalendar()
	def := domain.JobDefinition{ID: "retry-clock", JobType: "COMMAND", BusinessTime: &c, Retries: 1, RetryDelayMin: 72 * 60, Schedule: domain.Schedule{Enabled: true, KeepActive: 4}}
	s.defs = []domain.JobDefinition{def}
	if s.RunDaily("2026-03-07") != 1 {
		t.Fatal("ordem ausente")
	}
	id := "retry-clock-2026-03-07"
	s.startInstance(id, def)
	s.Stop() // encerra mock; o resultado controlado vem abaixo.
	var actual time.Time
	if err := s.db.QueryRow("SELECT started_at FROM instances WHERE id=?", id).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if !actual.Equal(start) {
		t.Fatalf("claim usou relógio real: %v", actual)
	}
	stamp.Store(start.Add(time.Minute).UnixNano())
	s.FinishInstance(id, domain.StatusNotOK, 1, "retry")
	want := start.Add(time.Minute + 72*time.Hour)
	var scheduled time.Time
	_ = s.db.QueryRow("SELECT scheduled_at FROM instances WHERE id=?", id).Scan(&scheduled)
	if !scheduled.Equal(want) {
		t.Fatalf("retry %v esperado %v", scheduled, want)
	}
	s.defs = nil
	for _, date := range []string{"2026-03-08", "2026-03-09", "2026-03-10"} {
		if _, err := s.carryOver(date); err != nil {
			t.Fatal(err)
		}
	}
	var active, origin string
	_ = s.db.QueryRow("SELECT order_date,carried_from,scheduled_at FROM instances WHERE id=?", id).Scan(&active, &origin, &scheduled)
	if active != "2026-03-10" || origin != "2026-03-07" || !scheduled.Equal(want) {
		t.Fatalf("carry %s %s %v", active, origin, scheduled)
	}
	r := instRow{ID: id, DefID: def.ID, OrderDate: active, CarriedFrom: origin, ScheduledAt: scheduled}
	if b := s.gateInstance(r, def, nil, want.Add(-time.Second), false); len(b) == 0 || b[0].Kind != GateWindow {
		t.Fatalf("retry antecipado: %v", b)
	}
	if b := s.gateInstance(r, def, nil, want, false); len(b) != 0 {
		t.Fatalf("retry no prazo: %v", b)
	}
	if ctx := s.buildVarContext(def, id); ctx.Runtime["ODATE"] != "20260307" {
		t.Fatalf("ODAT alterado: %v", ctx.Runtime)
	}
}
