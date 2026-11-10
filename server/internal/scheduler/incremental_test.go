package scheduler

import (
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

func TestIncrementalConditionWakeReconcileAndCorruptSnapshot(t *testing.T) {
	s := newTestScheduler(t)
	s.DemoMode = false
	s.EnableIncremental()
	s.AttachConditions(NewConditionEngine(s.db))
	now := s.Now()
	s.nowFn = func() time.Time { return now }
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 600 {
		id := fmt.Sprintf("job-%04d", i)
		def := domain.JobDefinition{ID: id, Team: "test", JobType: "COMMAND", ConditionsIn: []string{fmt.Sprintf("cond-%04d", i)}}
		raw, _ := json.Marshal(def)
		if _, err = tx.Exec("INSERT INTO instances(id,definition_id,team,order_date,status,scheduled_at,definition_snapshot) VALUES(?,?,?,?,'WAITING',?,?)", id, id, "test", s.TodayDate(), now.Add(-time.Minute), string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// O orçamento de 100ms pode adiar candidatos num host concorrido; número
	// fixo de ticks não é contrato. Exige convergência limitada e sem perdas.
	settle := func(tracked int, pages uint64) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if err = s.tickOnce(); err != nil {
				t.Fatal(err)
			}
			progress := s.IncrementalProgress()
			if progress.Pending == 0 && progress.Tracked == tracked && progress.Pages >= pages && progress.ReconciledAt.Equal(now) {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("fila incremental não convergiu: %+v", s.IncrementalProgress())
	}
	// Reconciliação continua limitada a três páginas para as 600 linhas.
	settle(600, 3)
	c := s.incremental
	c.mu.Lock()
	before := c.evaluated
	pages := c.pages
	cursor := c.cursor
	c.mu.Unlock()
	if before < 600 || pages != 3 || cursor != "" {
		t.Fatalf("bootstrap/paginação: %d %d %s", before, pages, cursor)
	}
	if err = s.tickOnce(); err != nil {
		t.Fatal(err)
	}
	if c.evaluated != before {
		t.Fatal("quiescência reavaliou instâncias")
	}
	if err = s.conditions.Set("cond-0007", s.TodayDate(), "test"); err != nil {
		t.Fatal(err)
	}
	if err = s.tickOnce(); err != nil {
		t.Fatal(err)
	}
	if c.evaluated != before+1 {
		t.Fatalf("condição despertou %d consumidores", c.evaluated-before)
	}
	// Atualização externa sem notificação é recuperada por uma nova passagem.
	if _, err = s.db.Exec("UPDATE instances SET status='HELD' WHERE id='job-0009'"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM scheduler_changes"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	settle(599, pages+3)
	if c.tracked["job-0009"] != nil {
		t.Fatal("reconciliação não recuperou evento perdido")
	}
	// Conteúdo novo nunca reutiliza plano validado antigo.
	rows, err := s.readTickInstances(" WHERE i.id='job-0007'")
	if err != nil {
		t.Fatal(err)
	}
	cached := s.prepareCandidate(rows[0], nil)
	if integrityBlock(cached) != "" {
		t.Fatal("snapshot válido recusado")
	}
	changed := rows[0]
	changed.Snapshot = "{broken"
	if integrityBlock(s.prepareCandidate(changed, nil)) == "" {
		t.Fatal("cache ocultou corrupção")
	}
}

func TestIncrementalScopeTimersAndParity(t *testing.T) {
	s := newTestScheduler(t)
	s.DemoMode = false
	s.EnableIncremental()
	s.AttachConditions(NewConditionEngine(s.db))
	now := s.Now()
	s.nowFn = func() time.Time { return now }
	r := instRow{ID: "one", DefID: "one", OrderDate: s.TodayDate(), Status: "WAITING", ScheduledAt: now.Add(time.Minute)}
	def := domain.JobDefinition{ID: "one", JobType: "COMMAND", ConditionsIn: []string{"input@odat"}}
	raw, _ := json.Marshal(def)
	r.Snapshot = string(raw)
	prepared := s.prepareCandidate(r, nil)
	full := s.gateInstance(r, def, CondIndex{}, now, false)
	fast := s.gateInstance(prepared, def, CondIndex{}, now, false)
	a, _ := json.Marshal(full)
	b, _ := json.Marshal(fast)
	if string(a) != string(b) {
		t.Fatalf("divergência: %s != %s", a, b)
	}
	c := s.incremental
	c.observe(s, prepared, nil, CondIndex{}, now)
	if len(c.timers) != 1 || !c.timers[0].due.Equal(r.ScheduledAt) {
		t.Fatal("horário não indexado")
	}
	c.conditionChanged(condKey("input", "1900-01-01"))
	if len(c.queue) != 0 {
		t.Fatal("escopo errado despertou consumidor")
	}
	c.conditionChanged(condKey("input", ""))
	c.conditionChanged(condKey("input", ""))
	if len(c.queue) != 1 {
		t.Fatal("estática/dedup", c.queue)
	}
}

func BenchmarkSchedulerQuiescent28800(b *testing.B) {
	for _, incremental := range []bool{false, true} {
		name := "full"
		if incremental {
			name = "incremental"
		}
		b.Run(name, func(b *testing.B) {
			d, err := db.Open(db.SQLite, filepath.Join(b.TempDir(), "state.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer d.Close()
			if err = db.Migrate(d); err != nil {
				b.Fatal(err)
			}
			s := New(storage.NewFileStore(b.TempDir(), false), d, hub.New(), time.Second)
			defer s.Stop()
			now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
			s.nowFn = func() time.Time { return now }
			if incremental {
				s.EnableIncremental()
			}
			tx, err := d.Begin()
			if err != nil {
				b.Fatal(err)
			}
			stmt, err := tx.Prepare("INSERT INTO instances(id,definition_id,team,order_date,status,scheduled_at,definition_snapshot) VALUES(?,?,?,'2026-11-09','WAITING',?,?)")
			if err != nil {
				b.Fatal(err)
			}
			for i := range 28800 {
				id := fmt.Sprintf("job-%05d", i)
				raw, _ := json.Marshal(domain.JobDefinition{ID: id, Team: "test", JobType: "COMMAND", Confirm: true})
				if _, err = stmt.Exec(id, id, "test", now, string(raw)); err != nil {
					b.Fatal(err)
				}
			}
			stmt.Close()
			if err = tx.Commit(); err != nil {
				b.Fatal(err)
			}
			if incremental {
				for i := 0; i < 2000; i++ {
					if err = s.tickOnce(); err != nil {
						b.Fatal(err)
					}
					c := s.incremental
					if c.cursor == "" && !c.nextReconcile.IsZero() && len(c.queue) == 0 {
						break
					}
					if i == 1999 {
						b.Fatal("bootstrap não terminou")
					}
				}
			} else {
				if err = s.tickOnce(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err = s.tickOnce(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestIncrementalCandidateCreationFIFO(t *testing.T) {
	s := newTestScheduler(t)
	s.EnableIncremental()
	now := s.Now()
	for _, item := range []struct {
		id      string
		created time.Time
	}{{"z-older", now.Add(-time.Hour)}, {"a-newer", now}} {
		if _, err := s.db.Exec("INSERT INTO instances(id,definition_id,order_date,status,scheduled_at,created_at) VALUES(?,? ,?,'WAITING',?,?)", item.id, item.id, s.TodayDate(), now, item.created); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.tickCandidates(s.TodayDate(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "z-older" {
		t.Fatalf("candidatos ignoraram antiguidade no empate: %+v", rows)
	}
}
