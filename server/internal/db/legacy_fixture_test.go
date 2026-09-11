package db

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Fixtures congeladas no DDL de b88af2a, para upgrades futuros não gerarem a
// própria entrada legada a partir de migrations que tenham sido alteradas.
func TestLegacyFixturesFrozen(t *testing.T) {
	for _, dialect := range []Dialect{SQLite, Postgres} {
		migs := sqliteMigrations
		if dialect == Postgres {
			migs = pgMigrations
		}
		var script strings.Builder
		fmt.Fprintf(&script, "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at %s DEFAULT CURRENT_TIMESTAMP);\n", tsType(dialect))
		for _, m := range migs[:22] {
			for _, stmt := range splitStatements(m.sql) {
				script.WriteString(strings.TrimSpace(stmt) + ";\n")
			}
			fmt.Fprintf(&script, "INSERT INTO schema_migrations(version) VALUES(%d);\n", m.version)
		}
		path := "testdata/legacy-v22-" + string(dialect) + ".sql"
		if os.Getenv("REGENTE_GENERATE_LEGACY_FIXTURES") == "1" {
			if err := os.WriteFile(path, []byte(script.String()), 0644); err != nil {
				t.Fatal(err)
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ReplaceAll(string(raw), "\r\n", "\n") != script.String() {
			t.Fatalf("DDL histórico %s divergiu da fixture b88af2a; acrescente uma migration, não edite a antiga", dialect)
		}
	}
}
