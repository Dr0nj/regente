package scheduler

import (
	"encoding/json"
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/domain"
	"testing"
	"time"
)

func TestI06DispatchRolloverSnapshot(t *testing.T) {
	s := newTestScheduler(t)
	setSetting(t, s, "daily_timezone", "America/Sao_Paulo")
	setSetting(t, s, "daily_at", "06:00")
	now := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC) // 02:00 SP, diária de 29.
	s.SetClock(businessclock.Func(func() time.Time { return now }))
	s.defs = []domain.JobDefinition{{ID: "i06", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true, RunAt: "02:00"}, Confirm: true}}
	if n := s.RunDaily("2026-09-29"); n != 1 {
		t.Fatalf("criadas %d", n)
	}
	var scheduled time.Time
	var snap string
	if err := s.db.QueryRow("SELECT scheduled_at,definition_snapshot FROM instances WHERE id='i06-2026-09-29'").Scan(&scheduled, &snap); err != nil {
		t.Fatal(err)
	}
	if !scheduled.Equal(now) {
		t.Fatalf("scheduled=%v esperado=%v", scheduled, now)
	}
	var def domain.JobDefinition
	_ = json.Unmarshal([]byte(snap), &def)
	if def.BusinessTime == nil || def.BusinessTime.Timezone != "America/Sao_Paulo" {
		t.Fatal(snap)
	}
	s.tickOnce()
	var waits int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM instance_events WHERE instance_id='i06-2026-09-29' AND kind='wait'").Scan(&waits)
	if waits == 0 {
		t.Fatal("tick não selecionou a diária anterior antes da virada")
	}
	ex, err := s.Explain("i06-2026-09-29")
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Blockers) != 1 || ex.Blockers[0].Kind != GateConfirm {
		t.Fatalf("gates %v", ex.Blockers)
	}
	setSetting(t, s, "daily_timezone", "Asia/Tokyo")
	setSetting(t, s, "daily_at", "00:00")
	if got := computeScheduledAt(def, "2026-09-29"); !got.Equal(now) {
		t.Fatalf("setting reinterpretou ordem: %v", got)
	}
	ctx := s.buildVarContext(def, "i06-2026-09-29")
	if ctx.Runtime["ODATE"] != "20260929" {
		t.Fatalf("ODAT %v", ctx.Runtime)
	}
}
func TestI06WindowSimulationAndLegacy(t *testing.T) {
	s := newTestScheduler(t)
	c := businessclock.Calendar{Timezone: "America/Sao_Paulo", DailyAt: "06:00"}
	def := domain.JobDefinition{ID: "night", JobType: "COMMAND", BusinessTime: &c, Schedule: domain.Schedule{Enabled: true, WindowFrom: "23:00", WindowTo: "02:00"}, SLA: &domain.SLASpec{DeadlineHM: "02:30", ExpectedDurationMin: 5}}
	now := c.At("2026-09-29", "01:00")
	r := instRow{ID: "night", DefID: "night", OrderDate: "2026-09-30", CarriedFrom: "2026-09-29", ScheduledAt: computeScheduledAt(def, "2026-09-29")}
	if bs := s.gateInstance(r, def, nil, now, false); len(bs) != 0 {
		t.Fatalf("janela noturna bloqueada %v", bs)
	}
	if bs := s.gateInstance(r, def, nil, c.At("2026-09-29", "03:00"), false); len(bs) == 0 || bs[0].Kind != GateWindowClosed {
		t.Fatalf("janela não fechou %v", bs)
	}
	report := Forecast([]domain.JobDefinition{def}, nil, "2026-09-29", c)
	what := WhatIf([]domain.JobDefinition{def}, nil, "2026-09-29", nil, nil, c)
	if !report.Jobs[0].StartAt.Equal(r.ScheduledAt) || !what.Rows[0].BaseStart.Equal(r.ScheduledAt) {
		t.Fatalf("simulação divergiu %+v %+v", report, what)
	}
	def.BusinessTime = nil
	if bs := s.gateInstance(r, def, nil, now, false); len(bs) == 0 || bs[0].Kind != GateConfiguration {
		t.Fatalf("legado recebeu zona inventada %v", bs)
	}
}
