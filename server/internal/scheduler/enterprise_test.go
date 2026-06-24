package scheduler

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// Escala — materializar 10k+ definitions numa daily. Prova que o caminho crítico
// (RunDaily → N inserts idempotentes no estado durável) aguenta o volume-alvo de
// produção (10k+ jobs/dia) sem degradar, e mede o throughput. Roda contra SQLite
// real (in-process); o gargalo de produção (Postgres/rede) é medido à parte.
func TestScale_RunDaily10kDefinitions(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de escala em -short")
	}
	s := newTestScheduler(t)

	const n = 10_000
	defs := make([]domain.JobDefinition, n)
	for i := 0; i < n; i++ {
		defs[i] = domain.JobDefinition{
			ID:       fmt.Sprintf("job-%05d", i),
			Label:    fmt.Sprintf("Job %05d", i),
			JobType:  "COMMAND",
			Schedule: domain.Schedule{Enabled: true},
		}
	}
	s.mu.Lock()
	s.defs = defs
	s.mu.Unlock()

	const date = "2026-06-23"
	start := time.Now()
	created := s.RunDaily(date)
	elapsed := time.Since(start)

	if created != n {
		t.Fatalf("esperava %d instances materializadas, veio %d", n, created)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM instances WHERE order_date=?`, date).Scan(&count); err != nil {
		t.Fatalf("count instances: %v", err)
	}
	if count != n {
		t.Fatalf("estado durável tem %d instances, esperava %d", count, n)
	}
	t.Logf("escala: %d instances em %v (%.0f inst/s)", n, elapsed.Round(time.Millisecond), float64(n)/elapsed.Seconds())

	// Idempotência sob volume: re-rodar a MESMA daily não duplica nada.
	if again := s.RunDaily(date); again != 0 {
		t.Fatalf("re-rodar a daily deveria criar 0 (idempotente), criou %d", again)
	}
}

// Quotas HA — o ResourceTracker é in-memory e vive no líder; após restart/failover
// um novo líder precisa reconstruir o uso a partir das instances RUNNING (que ainda
// detêm os slots no mundo real), senão deixaria começar jobs acima da capacidade.
func TestQuotas_RebuildFromRunning(t *testing.T) {
	s := newTestScheduler(t)
	tracker := NewResourceTracker()
	tracker.SetCapacity("db", 5)
	s.AttachResources(tracker)

	// 2 instances RUNNING que detêm 2 slots de "db" cada (snapshot com Resources).
	for _, id := range []string{"a-2026-06-23", "b-2026-06-23"} {
		def := domain.JobDefinition{ID: id[:1], JobType: "COMMAND", Resources: map[string]int{"db": 2}}
		snap, _ := json.Marshal(def)
		if _, err := s.db.Exec(
			`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot) VALUES(?,?,?,?,?,?)`,
			id, id[:1], "2026-06-23", string(domain.StatusRunning), time.Now(), string(snap),
		); err != nil {
			t.Fatalf("insert RUNNING %s: %v", id, err)
		}
	}

	n, err := s.RebuildResourcesFromRunning()
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if n != 2 {
		t.Fatalf("esperava reconstruir 2 instances, veio %d", n)
	}

	// used = 4/5 → um job querendo 2 NÃO cabe (furaria a quota), querendo 1 cabe.
	var used int
	for _, rs := range tracker.Snapshot() {
		if rs.Name == "db" {
			used = rs.Used
		}
	}
	if used != 4 {
		t.Fatalf("após rebuild, used de 'db' deveria ser 4, veio %d", used)
	}
	if tracker.TryAcquire("novo-2", map[string]int{"db": 2}) {
		t.Fatal("com used=4/5, adquirir 2 deveria FALHAR (quota furaria)")
	}
	if !tracker.TryAcquire("novo-1", map[string]int{"db": 1}) {
		t.Fatal("com used=4/5, adquirir 1 deveria caber")
	}

	// Rebuild é idempotente: rodar de novo não duplica o uso.
	if _, err := s.RebuildResourcesFromRunning(); err != nil {
		t.Fatalf("rebuild 2: %v", err)
	}
	for _, rs := range tracker.Snapshot() {
		if rs.Name == "db" && rs.Used != 5 { // 4 do rebuild + 1 do TryAcquire("novo-1")
			t.Fatalf("rebuild não-idempotente: used=%d, esperava 5", rs.Used)
		}
	}
}
