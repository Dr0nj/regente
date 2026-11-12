package db

import (
	"context"
	"errors"
	"testing"
)

func TestPostgresLocksDoNotRewriteRows(t *testing.T) {
	d, _, _ := auditDB(t, Postgres, 100)
	var headBefore, queueBefore string
	if err := d.QueryRow("SELECT ctid::text FROM audit_head WHERE id=1").Scan(&headBefore); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow("SELECT ctid::text FROM execution_queue_lock WHERE id=1").Scan(&queueBefore); err != nil {
		t.Fatal(err)
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = tx.LockExecutionQueue(); err != nil {
		t.Fatal(err)
	}
	// Os locks novos conflitam com outros leitores FOR UPDATE e com os writers antigos.
	for _, query := range []string{
		"SELECT seq FROM audit_head WHERE id=1 FOR UPDATE NOWAIT",
		"SELECT marker FROM execution_queue_lock WHERE id=1 FOR UPDATE NOWAIT",
		"UPDATE audit_head SET seq=seq WHERE id=1",
		"UPDATE execution_queue_lock SET marker=marker WHERE id=1",
	} {
		other, e := d.Raw().BeginTx(context.Background(), nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = other.Exec("SET LOCAL lock_timeout='50ms'"); e != nil {
			other.Rollback()
			t.Fatal(e)
		}
		_, e = other.Exec(query)
		other.Rollback()
		var sqlState interface{ SQLState() string }
		if !errors.As(e, &sqlState) || sqlState.SQLState() != "55P03" {
			t.Fatalf("lock não serializou concorrente: %s: %v", query, e)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var headAfter, queueAfter string
	if err = d.QueryRow("SELECT ctid::text FROM audit_head WHERE id=1").Scan(&headAfter); err != nil {
		t.Fatal(err)
	}
	if err = d.QueryRow("SELECT ctid::text FROM execution_queue_lock WHERE id=1").Scan(&queueAfter); err != nil {
		t.Fatal(err)
	}
	if headBefore != headAfter || queueBefore != queueAfter {
		t.Fatalf("lock criou versões mortas: head %s -> %s, queue %s -> %s", headBefore, headAfter, queueBefore, queueAfter)
	}
	records, err := d.AuditRecords(0, 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("lock gerou evento fictício: %v %v", records, err)
	}
	if _, err = d.Exec("INSERT INTO variables(name,value) VALUES('after-lock','ok')"); err != nil {
		t.Fatal(err)
	}
	if err = d.VerifyAudit(); err != nil {
		t.Fatal(err)
	}
}
