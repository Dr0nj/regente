// Package db — state store do servidor, plugável entre SQLite e Postgres.
//
// F1 (2026-06-14): o mesmo código de negócio roda nos dois backends. O tipo
// *db.DB embrulha *sql.DB e reescreve as queries por dialeto:
//   - placeholders `?`  -> `$1,$2,...` no Postgres;
//   - `INSERT OR REPLACE` (SQLite) -> `INSERT ... ON CONFLICT DO UPDATE` (PG);
//   - InsertID usa `RETURNING id` (portável; LastInsertId não existe no PG).
//
// O schema é aplicado por um migration runner versionado (schema_migrations),
// com DDL por dialeto. V2 (AWS) pode acrescentar um adapter DynamoDB atrás de
// um repositório sem tocar no scheduler — o caminho de ports fica documentado.
package db

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // driver "pgx" (Postgres)
	_ "modernc.org/sqlite"             // driver "sqlite" (pure-Go, sem CGO)
)

// Dialect identifica o backend ativo.
type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

// DB embrulha *sql.DB aplicando o rebind por dialeto. Os métodos têm a mesma
// assinatura de *sql.DB, então os call-sites existentes não mudam.
type DB struct {
	*sql.DB
	dialect Dialect
}

// Dialect devolve o backend ativo (usado por features que dependem do backend,
// ex.: G1 leader election via advisory lock no Postgres).
func (d *DB) Dialect() Dialect { return d.dialect }

// Raw devolve o *sql.DB cru (escape hatch para casos específicos de backend).
func (d *DB) Raw() *sql.DB { return d.DB }

func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.DB.Exec(rebind(query, d.dialect), args...)
}

func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(rebind(query, d.dialect), args...)
}

func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.DB.QueryRow(rebind(query, d.dialect), args...)
}

// Begin abre uma transação que também reescreve as queries por dialeto.
func (d *DB) Begin() (*Tx, error) {
	tx, err := d.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, dialect: d.dialect}, nil
}

// InsertID executa um INSERT e devolve o id gerado, portável entre SQLite e
// Postgres via `RETURNING id` (LastInsertId não é suportado pelo Postgres).
func (d *DB) InsertID(query string, args ...any) (int64, error) {
	var id int64
	err := d.QueryRow(query+" RETURNING id", args...).Scan(&id)
	return id, err
}

// Tx — transação com o mesmo rebind por dialeto.
type Tx struct {
	*sql.Tx
	dialect Dialect
}

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(rebind(query, t.dialect), args...)
}

func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(rebind(query, t.dialect), args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.Tx.QueryRow(rebind(query, t.dialect), args...)
}

// ParseDialect normaliza a string da flag -db-driver.
func ParseDialect(s string) (Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "sqlite", "sqlite3":
		return SQLite, nil
	case "postgres", "postgresql", "pgx", "pg":
		return Postgres, nil
	default:
		return "", fmt.Errorf("db driver desconhecido %q (use sqlite|postgres)", s)
	}
}

// Open abre/cria a conexão para o dialeto pedido. Para SQLite, `dsn` é o path do
// arquivo; para Postgres, a connection string (ex.: postgres://user:pw@host/db).
func Open(dialect Dialect, dsn string) (*DB, error) {
	switch dialect {
	case Postgres:
		sdb, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, err
		}
		if err := sdb.Ping(); err != nil {
			return nil, fmt.Errorf("postgres ping: %w", err)
		}
		return &DB{DB: sdb, dialect: Postgres}, nil
	case SQLite, "":
		sdb, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		if _, err := sdb.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
			return nil, err
		}
		return &DB{DB: sdb, dialect: SQLite}, nil
	default:
		return nil, fmt.Errorf("dialeto não suportado %q", dialect)
	}
}

// ---------------------------------------------------------------------------
// Rebind por dialeto
// ---------------------------------------------------------------------------

func rebind(query string, dialect Dialect) string {
	if dialect != Postgres {
		return query
	}
	return toDollar(pgUpsert(query))
}

// toDollar troca `?` por `$1,$2,...` (placeholders do Postgres), ignorando `?`
// dentro de literais entre aspas simples.
func toDollar(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	inStr := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == '\'' {
			inStr = !inStr
		}
		if c == '?' && !inStr {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// pgConflict mapeia tabela -> cláusula ON CONFLICT para traduzir os poucos
// `INSERT OR REPLACE` do SQLite em upsert do Postgres.
var pgConflict = map[string]string{
	"conditions": "(name, scope_date) DO UPDATE SET set_at=EXCLUDED.set_at, set_by=EXCLUDED.set_by",
	"daily_runs": "(order_date) DO UPDATE SET started_at=EXCLUDED.started_at",
}

func pgUpsert(query string) string {
	const marker = "INSERT OR REPLACE INTO "
	idx := strings.Index(query, marker)
	if idx < 0 {
		return query
	}
	rest := query[idx+len(marker):]
	tbl := rest
	for j := 0; j < len(rest); j++ {
		if rest[j] == '(' || rest[j] == ' ' {
			tbl = rest[:j]
			break
		}
	}
	out := strings.Replace(query, "INSERT OR REPLACE INTO", "INSERT INTO", 1)
	if clause, ok := pgConflict[strings.TrimSpace(tbl)]; ok {
		out += " ON CONFLICT " + clause
	}
	return out
}

// ---------------------------------------------------------------------------
// Migrations versionadas (F1/F4)
// ---------------------------------------------------------------------------

type migration struct {
	version int
	sql     string
}

// Migrate aplica as migrations pendentes (idempotente) para o dialeto ativo.
func Migrate(d *DB) error {
	ddl := `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at ` + tsType(d.dialect) + ` DEFAULT CURRENT_TIMESTAMP)`
	if _, err := d.DB.Exec(ddl); err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := d.DB.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if rows.Scan(&v) == nil {
			applied[v] = true
		}
	}
	_ = rows.Close()

	migs := sqliteMigrations
	if d.dialect == Postgres {
		migs = pgMigrations
	}
	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		for _, stmt := range splitStatements(m.sql) {
			if _, err := d.DB.Exec(stmt); err != nil {
				return fmt.Errorf("migration v%d: %w", m.version, err)
			}
		}
		if _, err := d.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, m.version); err != nil {
			return fmt.Errorf("record migration v%d: %w", m.version, err)
		}
	}
	return nil
}

func tsType(d Dialect) string {
	if d == Postgres {
		return "TIMESTAMPTZ"
	}
	return "DATETIME"
}

// splitStatements quebra um script DDL em statements por ';' (o protocolo
// estendido do pgx não aceita múltiplos statements num Exec). O DDL aqui não
// contém ';' embutido.
func splitStatements(script string) []string {
	parts := strings.Split(script, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// idCol devolve a definição de PK auto-incremento por dialeto.
const (
	sqliteID = "INTEGER PRIMARY KEY AUTOINCREMENT"
	pgID     = "BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY"
)

// schemaV1 monta o DDL base (v1) com os tipos do dialeto. Consolida o schema
// atual inteiro (todas as colunas que antes vinham por ALTER). CREATE ... IF NOT
// EXISTS torna seguro rodar sobre uma DB já migrada pela versão antiga.
func schemaV1(idDef, ts string) string {
	return `
CREATE TABLE IF NOT EXISTS instances (
	id            TEXT PRIMARY KEY,
	definition_id TEXT NOT NULL,
	order_date    TEXT NOT NULL,
	status        TEXT NOT NULL,
	scheduled_at  ` + ts + ` NOT NULL,
	started_at    ` + ts + `,
	finished_at   ` + ts + `,
	agent_id      TEXT,
	exit_code     INTEGER,
	output        TEXT,
	forced        INTEGER DEFAULT 0,
	created_at    ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	definition_commit_sha TEXT,
	definition_snapshot   TEXT,
	attempts      INTEGER DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_instances_order_date ON instances(order_date);
CREATE INDEX IF NOT EXISTS idx_instances_status     ON instances(status);
CREATE INDEX IF NOT EXISTS idx_instances_def        ON instances(definition_id, order_date);

CREATE TABLE IF NOT EXISTS agents (
	id           TEXT PRIMARY KEY,
	capabilities TEXT NOT NULL,
	last_seen_at ` + ts + ` NOT NULL,
	online       INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS daily_runs (
	order_date  TEXT PRIMARY KEY,
	started_at  ` + ts + ` NOT NULL,
	finished_at ` + ts + `
);

CREATE TABLE IF NOT EXISTS instance_events (
	id          ` + idDef + `,
	instance_id TEXT NOT NULL,
	ts          ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	kind        TEXT NOT NULL,
	actor       TEXT,
	message     TEXT,
	git_commit_sha TEXT,
	pr_number   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_instance_events_inst ON instance_events(instance_id, ts);

CREATE TABLE IF NOT EXISTS users (
	id             ` + idDef + `,
	username       TEXT UNIQUE NOT NULL,
	password_hash  TEXT NOT NULL,
	role           TEXT NOT NULL DEFAULT 'viewer',
	created_at     ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	must_change_pw INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL,
	created_at ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	expires_at ` + ts + ` NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS folder_acls (
	user_id     INTEGER NOT NULL,
	folder_name TEXT NOT NULL,
	perms       TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (user_id, folder_name)
);

CREATE TABLE IF NOT EXISTS definition_audit (
	id             ` + idDef + `,
	ts             ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	actor          TEXT NOT NULL,
	action         TEXT NOT NULL,
	team           TEXT NOT NULL,
	definition_id  TEXT,
	git_commit_sha TEXT,
	pr_number      INTEGER,
	pr_url         TEXT,
	branch         TEXT,
	mode           TEXT
);
CREATE INDEX IF NOT EXISTS idx_def_audit_team_id ON definition_audit(team, definition_id, ts DESC);

CREATE TABLE IF NOT EXISTS conditions (
	name       TEXT NOT NULL,
	scope_date TEXT NOT NULL DEFAULT '',
	set_at     ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	set_by     TEXT,
	PRIMARY KEY (name, scope_date)
);
CREATE INDEX IF NOT EXISTS idx_conditions_scope ON conditions(scope_date);

CREATE TABLE IF NOT EXISTS sla_breaches (
	id            ` + idDef + `,
	instance_id   TEXT NOT NULL,
	definition_id TEXT NOT NULL,
	kind          TEXT NOT NULL,
	severity      TEXT NOT NULL DEFAULT 'warning',
	message       TEXT,
	detected_at   ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	notified      INTEGER DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sla_inst_kind ON sla_breaches(instance_id, kind);
CREATE INDEX IF NOT EXISTS idx_sla_detected ON sla_breaches(detected_at DESC);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS agent_tokens (
	id           ` + idDef + `,
	token        TEXT UNIQUE NOT NULL,
	label        TEXT NOT NULL DEFAULT '',
	created_at   ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	last_used_at ` + ts + `
);

CREATE TABLE IF NOT EXISTS variables (
	name       TEXT PRIMARY KEY,
	value      TEXT NOT NULL DEFAULT '',
	updated_at ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	updated_by TEXT
);

CREATE TABLE IF NOT EXISTS design_sessions (
	id               TEXT PRIMARY KEY,
	actor            TEXT NOT NULL,
	folders_json     TEXT NOT NULL DEFAULT '[]',
	new_folders_json TEXT NOT NULL DEFAULT '[]',
	base_sha         TEXT NOT NULL DEFAULT '',
	path             TEXT NOT NULL,
	created_at       ` + ts + ` NOT NULL,
	last_touch       ` + ts + ` NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_design_sessions_actor ON design_sessions(actor);
`
}

var sqliteMigrations = []migration{
	{version: 1, sql: schemaV1(sqliteID, "DATETIME")},
}

var pgMigrations = []migration{
	{version: 1, sql: schemaV1(pgID, "TIMESTAMPTZ")},
}
