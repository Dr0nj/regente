package scheduler

import (
	"testing"
	"time"
)

type i16Leader bool

func (l i16Leader) IsLeader() bool { return bool(l) }
func TestI16TickProgress(t *testing.T) {
	s := newTestScheduler(t)
	s.tickGuard.busy.Store(true)
	s.Tick()
	s.tickGuard.busy.Store(false)
	p := s.TickProgress()
	if p.Attempts != 1 || p.Overlap != 1 || !p.CompletedAt.IsZero() {
		t.Fatal(p)
	}
	s.leader = i16Leader(false)
	s.Tick()
	p = s.TickProgress()
	if p.Follower != 1 || !p.CompletedAt.IsZero() {
		t.Fatal(p)
	}
	s.leader = i16Leader(true)
	s.Tick()
	before := s.TickProgress()
	if before.Completed != 1 || before.CompletedAt.IsZero() {
		t.Fatal(before)
	}
	time.Sleep(time.Millisecond)
	s.db.Close()
	s.Tick()
	after := s.TickProgress()
	if after.Failed != 1 || after.Completed != 1 || !after.CompletedAt.Equal(before.CompletedAt) || !after.AttemptedAt.After(before.AttemptedAt) {
		t.Fatal(after)
	}
}
