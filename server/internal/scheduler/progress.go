package scheduler

import "time"

// Não confundir sucesso da varredura síncrona com término de jobs ou hooks assíncronos.
type TickProgress struct {
	AttemptedAt, CompletedAt                       time.Time
	Attempts, Completed, Failed, Overlap, Follower uint64
	Duration                                       time.Duration
}

func (s *Scheduler) TickProgress() TickProgress { s.mu.Lock(); defer s.mu.Unlock(); return s.progress }

func (s *Scheduler) observeEligible(id string) error {
	s.mu.Lock()
	seen := s.eligibleSeen[id]
	s.mu.Unlock()
	if seen {
		return nil
	}
	_, err := s.db.Exec("INSERT INTO instance_events(instance_id,kind,actor,message,ts) SELECT ?,'eligible','scheduler',?,? WHERE NOT EXISTS(SELECT 1 FROM instance_events WHERE instance_id=? AND kind='eligible')", id, "Business gates passed at "+s.Now().UTC().Format(time.RFC3339Nano), s.Now(), id)
	if err == nil {
		s.mu.Lock()
		if s.eligibleSeen == nil {
			s.eligibleSeen = map[string]bool{}
		}
		if len(s.eligibleSeen) < 10000 {
			s.eligibleSeen[id] = true
		}
		s.mu.Unlock()
	}
	return err
}
