package scheduler

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// Fase A — imutabilidade: defForInstance deve preferir o snapshot congelado e
// só cair na def viva quando não há snapshot (instances legadas).
func TestDefForInstance_PrefersSnapshot(t *testing.T) {
	live := map[string]domain.JobDefinition{"x": {ID: "x", Label: "LIVE"}}

	snap, _ := json.Marshal(domain.JobDefinition{ID: "x", Label: "FROZEN"})
	d, ok := defForInstance(instRow{DefID: "x", Snapshot: string(snap)}, live)
	if !ok || d.Label != "FROZEN" {
		t.Fatalf("esperava snapshot FROZEN, veio ok=%v label=%q", ok, d.Label)
	}

	d2, ok2 := defForInstance(instRow{DefID: "x"}, live)
	if !ok2 || d2.Label != "LIVE" {
		t.Fatalf("sem snapshot deveria cair na def viva LIVE, veio ok=%v label=%q", ok2, d2.Label)
	}

	if _, ok3 := defForInstance(instRow{DefID: "inexistente"}, live); ok3 {
		t.Fatal("def inexistente sem snapshot deveria retornar ok=false")
	}
}

// newTestScheduler — Scheduler real sobre SQLite num arquivo temporário.
func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	database, err := db.Open(db.SQLite, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(storage.NewFileStore(t.TempDir(), false), database, hub.New(), 10*time.Millisecond)
}

// R2 — watchdog: Tick() deve carimbar lastTickAt (loop de scheduling vivo).
func TestTick_UpdatesLastTick(t *testing.T) {
	s := newTestScheduler(t)
	if !s.LastTick().IsZero() {
		t.Fatal("lastTick deveria começar zero")
	}
	s.Tick()
	if s.LastTick().IsZero() {
		t.Fatal("Tick() deveria atualizar lastTick")
	}
	if age := time.Since(s.LastTick()); age > time.Second {
		t.Fatalf("lastTick deveria ser recente, idade=%v", age)
	}
}

// panicBus — Bus cujo Dispatch explode, para exercitar o recover do dispatch.
type panicBus struct{}

func (panicBus) BroadcastWeb(string, interface{}) {}
func (panicBus) PickAgent(string) *hub.Client     { return nil }
func (panicBus) GetAgent(string) *hub.Client      { return nil }
func (panicBus) Dispatch(string, string, []byte) (hub.DispatchOutcome, string) {
	panic("boom no Dispatch")
}

// R2 — panic-recovery: um panic na goroutine de dispatch NÃO derruba o processo
// e a instance é finalizada como NOTOK (não fica pendurada em RUNNING).
func TestDispatch_PanicRecovered(t *testing.T) {
	s := newTestScheduler(t)
	s.hub = panicBus{} // injeta o Bus que explode no PickAgent

	def := domain.JobDefinition{ID: "p", Label: "Panic", JobType: "COMMAND"}
	snap, _ := json.Marshal(def)
	id := "p-2026-01-01"
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot) VALUES(?,?,?,?,?,?)`,
		id, "p", "2026-01-01", string(domain.StatusWaiting), time.Now(), string(snap),
	); err != nil {
		t.Fatalf("insert instance: %v", err)
	}

	s.startInstance(id, def) // se o recover falhar, o teste (processo) crasha aqui

	// O dispatch roda em goroutine; espera a transição terminal.
	deadline := time.Now().Add(2 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		_ = s.db.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status)
		if status == string(domain.StatusNotOK) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status != string(domain.StatusNotOK) {
		t.Fatalf("após panic no dispatch a instance deveria ser NOTOK, veio %q", status)
	}
}
