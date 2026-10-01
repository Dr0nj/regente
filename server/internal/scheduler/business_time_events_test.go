package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"testing"
	"time"
)

func TestI06EventClock(t *testing.T) {
	for _, queue := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "queue"}[queue], func(t *testing.T) {
			s := newTestScheduler(t)
			now := time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)
			s.SetClock(businessclock.Func(func() time.Time { return now }))
			if queue {
				s.StartEventQueue()
			}
			s.EmitEvent("clock-event", "test", "test", "clock")
			s.Stop()
			var got time.Time
			if err := s.db.QueryRow("SELECT ts FROM instance_events WHERE instance_id='clock-event'").Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Equal(now) {
				t.Fatalf("timeline usou relógio real: %v", got)
			}
		})
	}
}
