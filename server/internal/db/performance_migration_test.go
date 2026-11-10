package db

import (
	"context"
	"testing"
)

// Prova upgrade com trabalho em voo, incluindo recuperação UNCERTAIN, e
// atomicidade dos eventos usados como pistas pelo scheduler incremental.
func TestPerformanceMigrationV33(t *testing.T) {
	for _, dialect := range []Dialect{SQLite, Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d, _ := migrationTestDB(t, dialect)
			migrations := sqliteMigrations[:32]
			if dialect == Postgres {
				migrations = pgMigrations[:32]
			}
			if err := migrate(context.Background(), d, migrations, 32, 32); err != nil {
				t.Fatal(err)
			}
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := d.Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("INSERT INTO machine_principals(agent_id,environment,capabilities) VALUES('migration-agent','test','COMMAND')")
			states := []string{"planned", "accepted", "running", "uncertain", "succeeded", "failed", "cancelled"}
			for _, state := range states {
				exec("INSERT INTO lab_orders(id,source_instance_id,request_key,snapshot,snapshot_checksum,environment,state,created_at) VALUES(?,?,?,'{}','checksum','test',?,1)", state, state, state, state)
				exec("INSERT INTO execution_attempts(execution_id,order_id,attempt,fence,agent_id,request_key,intent,state,created_at) VALUES(?,?,1,1,'migration-agent',?,'start',?,1)", state, state, state, state)
			}
			if err := Migrate(d); err != nil {
				t.Fatal(err)
			}
			assertCount(t, d, "SELECT COUNT(*) FROM audit_reservations", 28)
			assertCount(t, d, "SELECT COUNT(*) FROM audit_reservations WHERE execution_id IN ('succeeded','failed','cancelled')", 0)
			if err := Migrate(d); err != nil {
				t.Fatal(err)
			}
			assertCount(t, d, "SELECT COUNT(*) FROM audit_reservations", 28)
			exec("INSERT INTO instances(id,definition_id,order_date,status,scheduled_at) VALUES('historical','job','2026-11-09','OK',CURRENT_TIMESTAMP)")
			assertCount(t, d, "SELECT COUNT(*) FROM scheduler_changes WHERE instance_id='historical'", 0)
			exec("UPDATE instances SET status='WAITING' WHERE id='historical'")
			assertCount(t, d, "SELECT COUNT(*) FROM scheduler_changes WHERE instance_id='historical'", 1)
			exec("DELETE FROM scheduler_changes WHERE instance_id='historical'")
			exec("UPDATE instances SET status='HELD' WHERE id='historical'")
			assertCount(t, d, "SELECT COUNT(*) FROM scheduler_changes WHERE instance_id='historical'", 1)
			exec("DELETE FROM scheduler_changes WHERE instance_id='historical'")
			exec("INSERT INTO instances(id,definition_id,order_date,status,scheduled_at) VALUES('changed','job','2026-11-09','WAITING',CURRENT_TIMESTAMP)")
			assertCount(t, d, "SELECT COUNT(*) FROM scheduler_changes WHERE instance_id='changed' AND version=1", 1)
			tx, err := d.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec("UPDATE instances SET confirmed=1 WHERE id='changed'"); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			assertCount(t, d, "SELECT COUNT(*) FROM scheduler_changes WHERE instance_id='changed' AND version=1", 1)
			exec("UPDATE instances SET confirmed=1 WHERE id='changed'")
			assertCount(t, d, "SELECT COUNT(*) FROM scheduler_changes WHERE instance_id='changed' AND version=2", 1)
			exec("DELETE FROM scheduler_changes WHERE instance_id='changed' AND version=1")
			assertCount(t, d, "SELECT COUNT(*) FROM scheduler_changes WHERE instance_id='changed' AND version=2", 1)
			exec("DELETE FROM instances WHERE id='changed'")
			assertCount(t, d, "SELECT COUNT(*) FROM scheduler_changes WHERE instance_id='changed' AND version=3", 1)
			if err = migrate(context.Background(), d, migrations, 32, 32); err == nil {
				t.Fatal("runtime antigo aceitou schema 33")
			}
		})
	}
}

// Só projeções exatas são telemetria; ampliar o SET não pode ocultar transições.
func TestAuditTelemetryDoesNotHideStateChanges(t *testing.T) {
	for _, query := range []string{
		"UPDATE execution_attempts SET lease_until=?,last_contact=? WHERE execution_id=? AND state=?",
		"UPDATE execution_attempts SET output_bytes=output_bytes+?,last_output_seq=? WHERE execution_id=? AND state=? AND last_output_seq=?",
	} {
		if _, mandatory := mutation(query); mandatory {
			t.Fatal("telemetria exige orçamento de conclusão", query)
		}
	}
	for _, query := range []string{
		"UPDATE execution_attempts SET lease_until=?,last_contact=?,state=? WHERE execution_id=?",
		"UPDATE execution_attempts SET output_bytes=?,result_checksum=? WHERE execution_id=?",
		"UPDATE lab_orders SET fence=fence+1 WHERE id=?",
	} {
		if _, mandatory := mutation(query); !mandatory {
			t.Fatal("transição saiu da trilha obrigatória", query)
		}
	}
}
