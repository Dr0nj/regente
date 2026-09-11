package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Faixa do RUNTIME após aplicar upgrades conhecidos; 0..22 são entradas legadas.
const MinSupportedSchema = 23
const MaxSupportedSchema = 23

// Migrate tem prazo total limitado, incluindo espera pelo outro migrador.
func Migrate(d *DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return MigrateContext(ctx, d)
}

// MigrateContext deve terminar antes de iniciar qualquer serviço que use o schema.
func MigrateContext(ctx context.Context, d *DB) error {
	migs := sqliteMigrations
	if d.dialect == Postgres {
		migs = pgMigrations
	}
	return migrate(ctx, d, migs, MinSupportedSchema, MaxSupportedSchema)
}

func migrationChecksum(m migration) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(m.sql, "\r\n", "\n"))))
}

func migrate(ctx context.Context, d *DB, migs []migration, minVersion, maxVersion int) error {
	if len(migs) == 0 || minVersion < 1 || minVersion > maxVersion || len(migs) != maxVersion {
		return fmt.Errorf("invalid binary migration range")
	}
	for i, m := range migs {
		if m.version != i+1 || strings.TrimSpace(m.sql) == "" {
			return fmt.Errorf("invalid binary migration sequence at v%d", m.version)
		}
	}
	for {
		done, err := migrationStep(ctx, d, migs, minVersion, maxVersion)
		if err != nil {
			return fmt.Errorf("schema upgrade stopped (restore a verified backup if legacy state is incomplete): %w", err)
		}
		if done {
			return nil
		}
	}
}

// Uma conexão fixa contém lock, DDL, backfill e histórico. BEGIN IMMEDIATE
// reserva o único writer SQLite ANTES de ler versões; no PG o advisory lock
// transacional não se confunde com o lock de liderança do scheduler.
func migrationStep(ctx context.Context, d *DB, migs []migration, minVersion, maxVersion int) (bool, error) {
	c, err := d.Raw().Conn(ctx)
	// O driver aplica journal_mode ao abrir a conexão; em banco vazio isso
	// também pode disputar o writer, antes mesmo de BEGIN IMMEDIATE.
	for sqliteBusy(d.dialect, err) {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
		c, err = d.Raw().Conn(ctx)
	}
	if err != nil {
		return false, err
	}
	defer c.Close()
	if err := beginMigration(ctx, c, d.dialect); err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := c.ExecContext(cleanup, "ROLLBACK"); err != nil {
				// Nunca devolver ao pool uma conexão com estado transacional incerto.
				_ = c.Raw(func(any) error { return driver.ErrBadConn })
			}
		}
	}()
	if d.dialect == Postgres {
		if _, err := c.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database() || ':' || current_schema() || ':regente:migrations', 0))`); err != nil {
			return false, fmt.Errorf("migration lock: %w", err)
		}
	}
	current, err := migrationHistory(ctx, c, d.dialect, migs)
	if err != nil {
		return false, err
	}
	done := current == len(migs)
	if done {
		if current < minVersion || current > maxVersion {
			return false, fmt.Errorf("schema v%d outside binary range [%d,%d]", current, minVersion, maxVersion)
		}
	} else {
		m := migs[current]
		for i, stmt := range splitStatements(m.sql) {
			if _, err := c.ExecContext(ctx, stmt); err != nil {
				return false, fmt.Errorf("migration v%d statement %d: %w", m.version, i+1, err)
			}
		}
		if _, err := c.ExecContext(ctx, rebind(`INSERT INTO schema_migrations(version) VALUES(?)`, d.dialect), m.version); err != nil {
			return false, err
		}
		if err := recordChecksum(ctx, c, d.dialect, m, "applied"); err != nil {
			return false, err
		}
	}
	if _, err := c.ExecContext(ctx, "COMMIT"); err != nil {
		return false, fmt.Errorf("migration commit: %w", err)
	}
	committed = true
	return done, nil
}

func beginMigration(ctx context.Context, c *sql.Conn, dialect Dialect) error {
	stmt := "BEGIN"
	if dialect == SQLite {
		stmt = "BEGIN IMMEDIATE"
	}
	for {
		_, err := c.ExecContext(ctx, stmt)
		if !sqliteBusy(dialect, err) {
			return err
		}
		// SQLITE_BUSY é contenção, não um erro de DDL; só BEGIN é repetido.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func sqliteBusy(dialect Dialect, err error) bool {
	var code interface{ Code() int }
	return err != nil && dialect == SQLite && errors.As(err, &code) && code.Code()&255 == 5
}

func migrationHistory(ctx context.Context, c *sql.Conn, dialect Dialect, migs []migration) (int, error) {
	if _, err := c.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at `+tsType(dialect)+` DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return 0, err
	}
	rows, err := c.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return 0, err
	}
	current := 0
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return 0, err
		}
		if v != current+1 || v > len(migs) {
			rows.Close()
			return 0, fmt.Errorf("incompatible migration history: v%d after v%d (binary maximum %d)", v, current, len(migs))
		}
		current = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='schema_migration_checksums')`
	if dialect == Postgres {
		query = `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='schema_migration_checksums')`
	}
	if err := c.QueryRowContext(ctx, query).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		// v23 faz backfill sem chave única por instance. Repetir sobre uma
		// execução parcial do runner antigo duplicaria estatísticas.
		for _, probe := range []struct {
			version int
			table   string
		}{{1, "instances"}, {23, "instance_runs"}} {
			if current >= probe.version {
				continue
			}
			var present bool
			q := `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`
			if dialect == Postgres {
				q = `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=?)`
			}
			if err := c.QueryRowContext(ctx, rebind(q, dialect), probe.table).Scan(&present); err != nil {
				return 0, err
			}
			if present {
				return 0, fmt.Errorf("unrecorded legacy schema v%d (%s exists); explicit recovery required", probe.version, probe.table)
			}
		}
		if _, err := c.ExecContext(ctx, `CREATE TABLE schema_migration_checksums (
			version INTEGER PRIMARY KEY REFERENCES schema_migrations(version),
			checksum TEXT NOT NULL, provenance TEXT NOT NULL,
			recorded_at `+tsType(dialect)+` NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
			return 0, err
		}
		for _, m := range migs[:current] {
			if err := recordChecksum(ctx, c, dialect, m, "legacy-adopted"); err != nil {
				return 0, err
			}
		}
	}
	rows, err = c.QueryContext(ctx, `SELECT version, checksum, provenance FROM schema_migration_checksums ORDER BY version`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var v int
		var hash, provenance string
		if err := rows.Scan(&v, &hash, &provenance); err != nil {
			return 0, err
		}
		if v != n+1 || v > current || hash != migrationChecksum(migs[v-1]) || (provenance != "applied" && provenance != "legacy-adopted") {
			return 0, fmt.Errorf("migration checksum/history mismatch at v%d", v)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if n != current {
		return 0, fmt.Errorf("missing migration checksums: %d records for %d versions", n, current)
	}
	return current, nil
}

func recordChecksum(ctx context.Context, c *sql.Conn, dialect Dialect, m migration, provenance string) error {
	_, err := c.ExecContext(ctx, rebind(`INSERT INTO schema_migration_checksums(version,checksum,provenance) VALUES(?,?,?)`, dialect), m.version, migrationChecksum(m), provenance)
	return err
}
