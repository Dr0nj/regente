package execution

import (
	"errors"
	"github.com/Dr0nj/regente-server/internal/db"
	"path/filepath"
	"testing"
)

func TestAuditReservedLifecycleSurvivesFullQueueAndRestart(t *testing.T) {
	f := newFixture(t)
	key := filepath.Join(t.TempDir(), "key")
	if err := f.d.EnableAudit(key, 10); err != nil {
		t.Fatal(err)
	}
	o := f.order(t, "order")
	a := f.start(t, o, "start")
	// Esgota orçamento livre, preservando os créditos da execução admitida.
	for {
		_, err := f.d.Exec("INSERT INTO variables(name,value) VALUES(?,?)", "fill", "x")
		if errors.Is(err, db.ErrAuditFull) {
			break
		}
		if err != nil { // mesma chave nas iterações: usa update auditável
			_, err = f.d.Exec("UPDATE variables SET value='y' WHERE name='fill'")
			if errors.Is(err, db.ErrAuditFull) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	f.reopen(t)
	if err := f.d.EnableAudit(key, 10); err != nil {
		t.Fatal(err)
	}
	f.running(t, a)
	if _, err := f.e.Acknowledge(identity(a), "heartbeat"); err != nil {
		t.Fatal("heartbeat sem orçamento livre", err)
	}
	if _, err := f.e.Append(Output{Identity: identity(a), Seq: 1, Chunk: "output"}); err != nil {
		t.Fatal("output bloqueou conclusão reservada", err)
	}
	before := count(t, f.d, "SELECT COUNT(*) FROM security_audit")
	for range 20 {
		r, err := f.e.Acknowledge(identity(a), "started")
		if err != nil || !r.Duplicate {
			t.Fatal(r, err)
		}
	}
	if got := count(t, f.d, "SELECT COUNT(*) FROM security_audit"); got != before {
		t.Fatalf("retry criou %d registros", got-before)
	}
	result := Result{Identity: identity(a), Output: "done"}
	if _, err := f.e.Complete(result); err != nil {
		t.Fatal(err)
	}
	if count(t, f.d, "SELECT COUNT(*) FROM audit_reservations") != 0 {
		t.Fatal("reservas vazaram")
	}
	f.reopen(t)
	if err := f.d.EnableAudit(key, 10); err != nil {
		t.Fatal(err)
	}
	r, err := f.e.Complete(result)
	if err != nil || !r.Duplicate {
		t.Fatal(r, err)
	}
	result.Output = "conflict"
	if _, err = f.e.Complete(result); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = f.d.VerifyAudit(); err != nil {
		t.Fatal(err)
	}
}

func TestAuditAdmissionRollsBackWithoutCapacity(t *testing.T) {
	f := newFixture(t)
	if err := f.d.EnableAudit(filepath.Join(t.TempDir(), "key"), 8); err != nil {
		t.Fatal(err)
	}
	o := f.order(t, "order")
	if _, err := f.e.Start(o.ID, "agent-a", "start", "start"); !errors.Is(err, db.ErrAuditFull) {
		t.Fatal(err)
	}
	if count(t, f.d, "SELECT COUNT(*) FROM execution_attempts") != 0 || count(t, f.d, "SELECT COUNT(*) FROM audit_reservations") != 0 {
		t.Fatal("admissão parcial")
	}
}

func TestAuditReservationSurvivesFailedResultCommit(t *testing.T) {
	f := newFixture(t)
	if err := f.d.EnableAudit(filepath.Join(t.TempDir(), "key"), 20); err != nil {
		t.Fatal(err)
	}
	a := f.start(t, f.order(t, "order"), "start")
	f.running(t, a)
	reserved := count(t, f.d, "SELECT COUNT(*) FROM audit_reservations")
	if _, err := f.d.Raw().Exec("CREATE TRIGGER fail_audit BEFORE INSERT ON security_audit BEGIN SELECT RAISE(ABORT,'synthetic storage failure'); END"); err != nil {
		t.Fatal(err)
	}
	result := Result{Identity: identity(a), Output: "finished"}
	if _, err := f.e.Complete(result); err == nil {
		t.Fatal("resultado confirmado sem evidência")
	}
	current, err := f.e.Attempt(a.ExecutionID)
	if err != nil || current.State != "running" {
		t.Fatal("estado parcial", current, err)
	}
	if count(t, f.d, "SELECT COUNT(*) FROM audit_reservations") != reserved {
		t.Fatal("rollback perdeu reserva")
	}
	if _, err = f.d.Raw().Exec("DROP TRIGGER fail_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.Complete(result); err != nil {
		t.Fatal(err)
	}
}
