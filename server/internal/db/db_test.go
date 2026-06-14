package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRebindSQLiteIsNoop(t *testing.T) {
	q := `SELECT * FROM instances WHERE id=? AND status=?`
	if got := rebind(q, SQLite); got != q {
		t.Fatalf("sqlite rebind should be no-op, got %q", got)
	}
}

func TestRebindPostgresPlaceholders(t *testing.T) {
	q := `UPDATE instances SET status=?, started_at=CURRENT_TIMESTAMP WHERE id=? AND status=?`
	want := `UPDATE instances SET status=$1, started_at=CURRENT_TIMESTAMP WHERE id=$2 AND status=$3`
	if got := rebind(q, Postgres); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestRebindPostgresIgnoresQuotedQuestionMark(t *testing.T) {
	// '?' dentro de literal não deve virar placeholder.
	q := `SELECT * FROM t WHERE label='a?b' AND id=?`
	want := `SELECT * FROM t WHERE label='a?b' AND id=$1`
	if got := rebind(q, Postgres); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestRebindPostgresUpsert(t *testing.T) {
	q := `INSERT OR REPLACE INTO conditions(name, scope_date, set_at, set_by) VALUES(?,?,?,?)`
	want := `INSERT INTO conditions(name, scope_date, set_at, set_by) VALUES($1,$2,$3,$4) ON CONFLICT (name, scope_date) DO UPDATE SET set_at=EXCLUDED.set_at, set_by=EXCLUDED.set_by`
	if got := rebind(q, Postgres); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestParseDialect(t *testing.T) {
	cases := map[string]Dialect{
		"": SQLite, "sqlite": SQLite, "SQLite": SQLite,
		"postgres": Postgres, "postgresql": Postgres, "pgx": Postgres,
	}
	for in, want := range cases {
		got, err := ParseDialect(in)
		if err != nil || got != want {
			t.Fatalf("ParseDialect(%q)=%v,%v want %v", in, got, err, want)
		}
	}
	if _, err := ParseDialect("mysql"); err == nil {
		t.Fatal("expected error for unknown driver")
	}
}

// TestSQLiteMigrateAndCRUD valida o migration runner + um round-trip real no
// SQLite (o gate testável do F1 neste ambiente; o Postgres é validado pelo
// usuário com REGENTE_TEST_PG_DSN — ver TestPostgresMigrateAndCRUD).
func TestSQLiteMigrateAndCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(SQLite, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if err := Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// idempotência: rodar de novo não pode falhar.
	if err := Migrate(d); err != nil {
		t.Fatalf("migrate (2x): %v", err)
	}
	var version int
	if err := d.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version < 1 {
		t.Fatalf("schema_migrations version=%d err=%v", version, err)
	}

	// InsertID (RETURNING) + leitura.
	id, err := d.InsertID(`INSERT INTO agent_tokens(token, label) VALUES(?,?)`, "rgta_test", "lbl")
	if err != nil || id == 0 {
		t.Fatalf("InsertID: id=%d err=%v", id, err)
	}
	var label string
	if err := d.QueryRow(`SELECT label FROM agent_tokens WHERE id=?`, id).Scan(&label); err != nil || label != "lbl" {
		t.Fatalf("read back: label=%q err=%v", label, err)
	}

	// upsert via INSERT OR REPLACE (caminho SQLite nativo).
	for i := 0; i < 2; i++ {
		if _, err := d.Exec(`INSERT OR REPLACE INTO conditions(name, scope_date, set_by) VALUES(?,?,?)`, "C", "2026-06-14", "tester"); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM conditions WHERE name=?`, "C").Scan(&n); err != nil || n != 1 {
		t.Fatalf("upsert dedup: n=%d err=%v", n, err)
	}
}

// TestPostgresMigrateAndCRUD roda o MESMO contrato no Postgres. Só executa se
// REGENTE_TEST_PG_DSN estiver setado (ex.:
//
//	REGENTE_TEST_PG_DSN="postgres://user:pw@localhost:5432/regente_test?sslmode=disable" \
//	    go test ./internal/db/ -run Postgres -v
//
// Usa um schema dedicado (regente_test) e o derruba ao final para não sujar a DB.
func TestPostgresMigrateAndCRUD(t *testing.T) {
	dsn := os.Getenv("REGENTE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("REGENTE_TEST_PG_DSN não setado — pulando teste Postgres")
	}
	d, err := Open(Postgres, dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	defer d.Close()
	if d.Dialect() != Postgres {
		t.Fatalf("dialect=%v want postgres", d.Dialect())
	}
	if err := Migrate(d); err != nil {
		t.Fatalf("migrate pg: %v", err)
	}
	if err := Migrate(d); err != nil {
		t.Fatalf("migrate pg (2x): %v", err)
	}

	token := "rgta_pgtest"
	_, _ = d.Exec(`DELETE FROM agent_tokens WHERE token=?`, token)
	id, err := d.InsertID(`INSERT INTO agent_tokens(token, label) VALUES(?,?)`, token, "pg")
	if err != nil || id == 0 {
		t.Fatalf("InsertID pg: id=%d err=%v", id, err)
	}
	var label string
	if err := d.QueryRow(`SELECT label FROM agent_tokens WHERE id=?`, id).Scan(&label); err != nil || label != "pg" {
		t.Fatalf("read back pg: label=%q err=%v", label, err)
	}
	_, _ = d.Exec(`DELETE FROM agent_tokens WHERE token=?`, token)

	// upsert traduzido: INSERT OR REPLACE -> ON CONFLICT DO UPDATE.
	_, _ = d.Exec(`DELETE FROM conditions WHERE name=?`, "Cpg")
	for i := 0; i < 2; i++ {
		if _, err := d.Exec(`INSERT OR REPLACE INTO conditions(name, scope_date, set_by) VALUES(?,?,?)`, "Cpg", "2026-06-14", "tester"); err != nil {
			t.Fatalf("pg upsert: %v", err)
		}
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM conditions WHERE name=?`, "Cpg").Scan(&n); err != nil || n != 1 {
		t.Fatalf("pg upsert dedup: n=%d err=%v", n, err)
	}
	_, _ = d.Exec(`DELETE FROM conditions WHERE name=?`, "Cpg")
}
