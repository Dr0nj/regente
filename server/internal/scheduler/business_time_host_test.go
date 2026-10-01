package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/domain"
	"testing"
	"time"
)

func TestI06HostTimezoneIndependence(t *testing.T) {
	old := time.Local
	defer func() { time.Local = old }()
	for _, zone := range []string{"UTC", "Asia/Tokyo", "Pacific/Honolulu"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		time.Local = loc
		s := newTestScheduler(t)
		setSetting(t, s, "daily_timezone", "America/Sao_Paulo")
		setSetting(t, s, "daily_at", "06:00")
		s.SetClock(businessclock.Func(func() time.Time { return time.Date(2026, 9, 30, 8, 59, 59, 0, time.UTC) }))
		if got := s.TodayDate(); got != "2026-09-29" {
			t.Fatalf("host %s: %s", zone, got)
		}
		c := s.BusinessCalendar()
		d := domain.JobDefinition{BusinessTime: &c, Schedule: domain.Schedule{RunAt: "02:00"}}
		if got := computeScheduledAt(d, "2026-09-29").Format(time.RFC3339); got != "2026-09-30T05:00:00Z" {
			t.Fatalf("host %s: %s", zone, got)
		}
	}
}
