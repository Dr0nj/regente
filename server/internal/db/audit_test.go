package db

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func auditDB(t *testing.T, dialect Dialect, capacity int64) (*DB, string, string) {
	t.Helper()
	d, dsn := migrationTestDB(t, dialect)
	if err := Migrate(d); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "audit.key")
	if err := d.EnableAudit(key, capacity); err != nil {
		t.Fatal(err)
	}
	return d, dsn, key
}
func TestMandatoryAuditTransactions(t *testing.T) {
	for _, dialect := range []Dialect{SQLite, Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			t.Run("exec_returning_prepared_no_secrets", func(t *testing.T) {
				d, _, _ := auditDB(t, dialect, 100)
				actor := d.WithAuditIdentity(AuditIdentity{Actor: "alice", Route: "POST /api/users"})
				id, err := actor.InsertID("INSERT INTO users(username,password_hash) VALUES(?,?)", "target", "sentinel-secret")
				if err != nil || id < 1 {
					t.Fatal(id, err)
				}
				tx, err := actor.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				stmt, err := tx.Prepare("INSERT INTO variables(name,value) VALUES(?,?)")
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"a", "b", "c"} {
					if _, err = stmt.Exec(name, "sentinel-secret"); err != nil {
						t.Fatal(err)
					}
				}
				stmt.Close()
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
				records, err := d.AuditRecords(0, 10)
				if err != nil || len(records) != 2 {
					t.Fatal(len(records), err)
				}
				for _, r := range records {
					if strings.Contains(r.Payload, "sentinel-secret") {
						t.Fatal("segredo na trilha")
					}
					var p AuditPayload
					if err = json.Unmarshal([]byte(r.Payload), &p); err != nil || p.Identity.Actor != "alice" {
						t.Fatal(p, err)
					}
				}
				var p AuditPayload
				_ = json.Unmarshal([]byte(records[1].Payload), &p)
				if len(p.Mutations) != 1 || p.Mutations[0].Count != 3 {
					t.Fatal(p)
				}
				if err = d.VerifyAudit(); err != nil {
					t.Fatal(err)
				}
			})
			t.Run("persistence_failure_rolls_back", func(t *testing.T) {
				d, _, _ := auditDB(t, dialect, 10)
				if _, err := d.Raw().Exec("DROP TABLE audit_delivery"); err != nil {
					t.Fatal(err)
				}
				_, err := d.Exec("INSERT INTO variables(name,value) VALUES(?,?)", "refused", "secret")
				if !errors.Is(err, ErrAuditUnavailable) {
					t.Fatal(err)
				}
				assertCount(t, d, "SELECT COUNT(*) FROM variables WHERE name='refused'", 0)
				assertCount(t, d, "SELECT COUNT(*) FROM security_audit", 0)
			})
			t.Run("capacity_separate_from_telemetry_and_recovery", func(t *testing.T) {
				d, _, _ := auditDB(t, dialect, 1)
				if _, err := d.Exec("INSERT INTO variables(name,value) VALUES('first','one')"); err != nil {
					t.Fatal(err)
				}
				if _, err := d.Exec("INSERT INTO variables(name,value) VALUES('second','two')"); !errors.Is(err, ErrAuditFull) {
					t.Fatal(err)
				}
				assertCount(t, d, "SELECT COUNT(*) FROM variables", 1)
				if _, err := d.Exec("INSERT INTO instance_events(instance_id,kind,message) VALUES('i','telemetry','hello')"); err != nil {
					t.Fatal(err)
				}
				if err := d.AuditObservation("access.denied", "403"); err != nil {
					t.Fatal(err)
				}
				tx, err := d.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if err = tx.AuditAdministrative("export.retry"); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
				assertCount(t, d, "SELECT COUNT(*) FROM security_audit", 3)
			})
			t.Run("restart_tamper_and_verified_restore", func(t *testing.T) {
				d, dsn, key := auditDB(t, dialect, 10)
				if _, err := d.Exec("INSERT INTO variables(name,value) VALUES('kept','one')"); err != nil {
					t.Fatal(err)
				}
				other, err := Open(dialect, dsn)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				if err = other.EnableAudit(key, 10); err != nil {
					t.Fatal(err)
				}
				records, err := other.AuditRecords(0, 10)
				if err != nil || len(records) != 1 {
					t.Fatal(records, err)
				}
				r := records[0]
				if _, err = d.Raw().Exec("UPDATE security_audit SET payload='changed'"); err != nil {
					t.Fatal(err)
				}
				if d.VerifyAudit() == nil {
					t.Fatal("adulteração não detectada")
				}
				if _, err = d.Raw().Exec(rebind("UPDATE security_audit SET payload=? WHERE seq=?", dialect), r.Payload, r.Seq); err != nil {
					t.Fatal(err)
				}
				if err = d.VerifyAudit(); err != nil {
					t.Fatal(err)
				}
				if err = d.EnableAudit(filepath.Join(t.TempDir(), "missing.key"), 10); err == nil {
					t.Fatal("regenerou chave sobre ledger existente")
				}
				if dialect == SQLite {
					backup := filepath.Join(t.TempDir(), "restore.db")
					if err = OnlineBackup(d, backup); err != nil {
						t.Fatal(err)
					}
					restored, err := Open(SQLite, backup)
					if err != nil {
						t.Fatal(err)
					}
					defer restored.Close()
					if err = restored.EnableAudit(key, 10); err != nil {
						t.Fatal(err)
					}
					assertCount(t, restored, "SELECT COUNT(*) FROM variables", 1)
				}
			})
		})
	}
}

// Processo filho encerra imediatamente após commit; não existe flush/graceful shutdown.
func TestMandatoryAuditCrash(t *testing.T) {
	if os.Getenv("REGENTE_AUDIT_CRASH_CHILD") == "1" {
		dialect := Dialect(os.Getenv("REGENTE_AUDIT_CRASH_DIALECT"))
		d, err := Open(dialect, os.Getenv("REGENTE_AUDIT_CRASH_DSN"))
		if err != nil {
			os.Exit(41)
		}
		if err = d.EnableAudit(os.Getenv("REGENTE_AUDIT_CRASH_KEY"), 10); err != nil {
			os.Exit(42)
		}
		if _, err = d.Exec("INSERT INTO variables(name,value) VALUES('crash-accepted','kept')"); err != nil {
			os.Exit(43)
		}
		os.Exit(77)
	}
	for _, dialect := range []Dialect{SQLite, Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d, dsn, key := auditDB(t, dialect, 10)
			cmd := exec.Command(os.Args[0], "-test.run=^TestMandatoryAuditCrash$")
			cmd.Env = append(os.Environ(), "REGENTE_AUDIT_CRASH_CHILD=1", "REGENTE_AUDIT_CRASH_DIALECT="+string(dialect), "REGENTE_AUDIT_CRASH_DSN="+dsn, "REGENTE_AUDIT_CRASH_KEY="+key)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 77 {
				t.Fatal(err, string(output))
			}
			assertCount(t, d, "SELECT COUNT(*) FROM variables WHERE name='crash-accepted'", 1)
			assertCount(t, d, "SELECT COUNT(*) FROM security_audit", 1)
			if err = d.VerifyAudit(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
