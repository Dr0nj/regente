package db

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Cada caso PG cria apenas seu schema aleatório; nunca apaga dados do chamador.
func migrationTestDB(t *testing.T, dialect Dialect) (*DB, string) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "state.db")
	if dialect == Postgres {
		dsn = os.Getenv("REGENTE_TEST_PG_DSN")
		if dsn == "" {
			if os.Getenv("REGENTE_REQUIRE_INTEGRATION") == "1" {
				t.Fatal("REGENTE_TEST_PG_DSN obrigatório no gate de integração")
			}
			t.Skip("Postgres real não configurado")
		}
		admin, err := Open(Postgres, dsn)
		if err != nil {
			t.Fatal(err)
		}
		schema := fmt.Sprintf("regente_test_%d", time.Now().UnixNano())
		if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
			admin.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			defer admin.Close()
			if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
				t.Error(err)
			}
		})
		u, err := url.Parse(dsn)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			t.Fatal("use URL postgres:// no DSN de teste")
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	}
	d, err := Open(dialect, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, dsn
}

func TestMigrationSafety(t *testing.T) {
	for _, dialect := range []Dialect{SQLite, Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			t.Run("concurrent_fresh_and_restart", func(t *testing.T) {
				d, dsn := migrationTestDB(t, dialect)
				other, err := Open(dialect, dsn)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				start := make(chan struct{})
				results := make(chan error, 2)
				for _, node := range []*DB{d, other} {
					go func(node *DB) { <-start; results <- Migrate(node) }(node)
				}
				close(start)
				for i := 0; i < 2; i++ {
					if err := <-results; err != nil {
						t.Fatal(err)
					}
				}
				if err := Migrate(d); err != nil {
					t.Fatal(err)
				}
				assertCount(t, d, `SELECT COUNT(*) FROM schema_migration_checksums WHERE provenance='applied'`, MaxSupportedSchema)
			})
			t.Run("legacy_v22_backfill_and_preservation", func(t *testing.T) {
				d, _ := migrationTestDB(t, dialect)
				legacyFixture(t, d, 22)
				if err := Migrate(d); err != nil {
					t.Fatal(err)
				}
				if err := Migrate(d); err != nil {
					t.Fatal(err)
				}
				assertCount(t, d, `SELECT COUNT(*) FROM instance_runs WHERE instance_id='legacy-running'`, 1)
				assertCount(t, d, `SELECT COUNT(*) FROM schema_migration_checksums WHERE provenance='legacy-adopted'`, 22)
				assertCount(t, d, `SELECT COUNT(*) FROM agent_tokens WHERE token='synthetic-legacy-token'`, 1)
				assertCount(t, d, `SELECT COUNT(*) FROM daily_runs WHERE finished_at IS NULL`, 1)
				assertCount(t, d, `SELECT COUNT(*) FROM design_sessions WHERE id='legacy-draft'`, 1)
			})
			t.Run("statement_failure_rolls_back_and_resumes", func(t *testing.T) {
				d, _ := migrationTestDB(t, dialect)
				migs := []migration{{1, `CREATE TABLE probe (id INTEGER PRIMARY KEY); INSERT INTO probe VALUES(1)`},
					{2, `INSERT INTO probe VALUES(2); CREATE TABLE partial_ddl (id INTEGER); INSERT INTO missing_table VALUES(1)`}}
				if err := migrate(context.Background(), d, migs, 2, 2); err == nil {
					t.Fatal("esperava falha na instrução 3")
				}
				assertCount(t, d, `SELECT COUNT(*) FROM probe`, 1)
				assertCount(t, d, `SELECT COUNT(*) FROM schema_migrations`, 1)
				// Se o CREATE anterior não foi revertido, esta retomada falha.
				migs[1].sql = `INSERT INTO probe VALUES(2); CREATE TABLE partial_ddl (id INTEGER)`
				if err := migrate(context.Background(), d, migs, 2, 2); err != nil {
					t.Fatal(err)
				}
				assertCount(t, d, `SELECT COUNT(*) FROM probe`, 2)
			})
			for _, damage := range []string{"checksum", "missing_checksum", "future", "gap", "changed_sql"} {
				t.Run("refuse_"+damage, func(t *testing.T) {
					d, _ := migrationTestDB(t, dialect)
					if err := Migrate(d); err != nil {
						t.Fatal(err)
					}
					query := map[string]string{
						"checksum":         `UPDATE schema_migration_checksums SET checksum='wrong' WHERE version=1`,
						"missing_checksum": `DELETE FROM schema_migration_checksums WHERE version=1`,
						"future":           `INSERT INTO schema_migrations(version) VALUES(24)`,
						"gap":              `INSERT INTO schema_migrations(version) VALUES(25)`,
					}[damage]
					if query != "" {
						if _, err := d.Exec(query); err != nil {
							t.Fatal(err)
						}
					}
					if damage == "changed_sql" {
						migs := append([]migration(nil), sqliteMigrations...)
						if dialect == Postgres {
							migs = append([]migration(nil), pgMigrations...)
						}
						migs[0].sql += "\n-- modified"
						if err := migrate(context.Background(), d, migs, 23, 23); err == nil {
							t.Fatal("aceitou SQL modificado")
						}
					} else if err := Migrate(d); err == nil {
						t.Fatal("aceitou histórico incompatível")
					}
				})
			}
			t.Run("legacy_partial_failure_is_not_silently_adopted", func(t *testing.T) {
				d, _ := migrationTestDB(t, dialect)
				legacyFixture(t, d, 22)
				// Simula v23 completa em autocommit, mas sem gravar a versão.
				m := sqliteMigrations[22]
				if dialect == Postgres {
					m = pgMigrations[22]
				}
				for _, stmt := range splitStatements(m.sql) {
					if _, err := d.Exec(stmt); err != nil {
						t.Fatal(err)
					}
				}
				if err := Migrate(d); err == nil {
					t.Fatal("esperava recusa de estado parcial")
				}
				assertCount(t, d, `SELECT COUNT(*) FROM schema_migrations`, 22)
				assertCount(t, d, `SELECT COUNT(*) FROM instance_runs`, 1)
			})
			t.Run("process_death_releases_transaction_and_lock", func(t *testing.T) {
				d, dsn := migrationTestDB(t, dialect)
				if err := Migrate(d); err != nil {
					t.Fatal(err)
				}
				marker := filepath.Join(t.TempDir(), "ready")
				cmd := exec.Command(os.Args[0], "-test.run=^TestMigrationCrashHelper$")
				cmd.Env = append(os.Environ(), "REGENTE_CRASH_DSN="+dsn, "REGENTE_CRASH_DIALECT="+string(dialect), "REGENTE_CRASH_MARKER="+marker)
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
				deadline := time.Now().Add(15 * time.Second)
				for {
					if _, err := os.Stat(marker); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("filho não entrou na transação")
					}
					time.Sleep(20 * time.Millisecond)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
				defer cancel()
				if err := MigrateContext(ctx, d); err == nil {
					t.Fatal("outro migrador ignorou o lock")
				}
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				_ = cmd.Wait()
				if err := Migrate(d); err != nil {
					t.Fatal(err)
				}
				// DDL e dado não commitados precisam desaparecer após morte real.
				if _, err := d.Exec(`CREATE TABLE crash_probe (id INTEGER)`); err != nil {
					t.Fatal(err)
				}
				assertCount(t, d, `SELECT COUNT(*) FROM settings WHERE key='crash-probe'`, 0)
			})
		})
	}
}

func TestMigrationCrashHelper(t *testing.T) {
	dsn := os.Getenv("REGENTE_CRASH_DSN")
	if dsn == "" {
		return
	}
	d, err := Open(Dialect(os.Getenv("REGENTE_CRASH_DIALECT")), dsn)
	if err != nil {
		t.Fatal(err)
	}
	c, err := d.Raw().Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := beginMigration(context.Background(), c, d.Dialect()); err != nil {
		t.Fatal(err)
	}
	if d.Dialect() == Postgres {
		if _, err := c.ExecContext(context.Background(), `SELECT pg_advisory_xact_lock(hashtextextended(current_database() || ':' || current_schema() || ':regente:migrations',0))`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.ExecContext(context.Background(), `CREATE TABLE crash_probe (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExecContext(context.Background(), `INSERT INTO settings(key,value) VALUES('crash-probe','uncommitted')`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("REGENTE_CRASH_MARKER"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Minute)
	t.Fatal("o pai deveria encerrar este processo")
}

func assertCount(t *testing.T, d *DB, query string, want int) {
	t.Helper()
	var got int
	if err := d.QueryRow(query).Scan(&got); err != nil || got != want {
		t.Fatalf("contagem=%d esperada=%d err=%v", got, want, err)
	}
}

func legacyFixture(t *testing.T, d *DB, version int) {
	t.Helper()
	migs := sqliteMigrations
	if d.Dialect() == Postgres {
		migs = pgMigrations
	}
	if _, err := d.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at ` + tsType(d.Dialect()) + ` DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range migs[:version] {
		for _, stmt := range splitStatements(m.sql) {
			if _, err := d.Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := d.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, m.version); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("testdata/legacy-data.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range splitStatements(string(raw)) {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationChecksumLineEndings(t *testing.T) {
	m := migration{1, "CREATE TABLE x(id INTEGER);\n"}
	other := migration{1, strings.ReplaceAll(m.sql, "\n", "\r\n")}
	if migrationChecksum(m) != migrationChecksum(other) {
		t.Fatal("checksum depende do checkout Windows/Linux")
	}
}

func TestMigrationLegacySQLiteBackupRestore(t *testing.T) {
	d, _ := migrationTestDB(t, SQLite)
	legacyFixture(t, d, 22)
	backup := filepath.Join(t.TempDir(), "before-upgrade.db")
	if err := OnlineBackup(d, backup); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(d); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(SQLite, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertCount(t, restored, `SELECT COUNT(*) FROM schema_migrations`, 22)
	if err := Migrate(restored); err != nil {
		t.Fatal(err)
	}
	assertCount(t, restored, `SELECT COUNT(*) FROM instance_runs WHERE instance_id='legacy-running'`, 1)
	assertCount(t, restored, `SELECT COUNT(*) FROM design_sessions WHERE id='legacy-draft'`, 1)
}
