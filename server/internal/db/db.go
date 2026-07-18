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

// Prepare cria um prepared statement com o rebind por dialeto, atrelado a esta
// transação. Use em loops quentes (ex.: materialização da daily em lote) para não
// re-parsear o SQL por linha — o statement é compilado uma vez e reexecutado N×.
// Sombreia o Prepare promovido do *sql.Tx (que NÃO faria o rebind p/ Postgres).
func (t *Tx) Prepare(query string) (*sql.Stmt, error) {
	return t.Tx.Prepare(rebind(query, t.dialect))
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
		// Pragmas via DSN (`_pragma=`), NÃO via Exec: o database/sql mantém um POOL
		// de conexões e um `Exec("PRAGMA busy_timeout...")` só aplica na conexão que
		// o executou — as demais ficavam sem busy_timeout e estouravam SQLITE_BUSY
		// em rajada (ex.: emitEvent perdendo eventos de auditoria ao materializar a
		// daily). journal_mode=WAL até persiste no arquivo, mas busy_timeout e
		// foreign_keys são POR-CONEXÃO; no DSN o driver aplica em cada conexão nova.
		sdb, err := sql.Open("sqlite", sqliteDSN(dsn))
		if err != nil {
			return nil, err
		}
		return &DB{DB: sdb, dialect: SQLite}, nil
	default:
		return nil, fmt.Errorf("dialeto não suportado %q", dialect)
	}
}

// sqliteDSN anexa os pragmas per-connection ao path do arquivo. O modernc/sqlite
// executa cada `_pragma=` na ABERTURA de cada conexão do pool (applyQueryParams).
// Se o chamador já configurou `_pragma=` explicitamente, respeita e não mexe.
func sqliteDSN(path string) string {
	if strings.Contains(path, "_pragma=") {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
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

// schemaV2 — Phase 8 alerting. Regras (toggleáveis) + eventos disparados.
// `ts_ms` guarda o epoch em ms para o frontend renderizar sem parse de timezone.
func schemaV2(idDef, ts string) string {
	return `
CREATE TABLE IF NOT EXISTS alert_rules (
	id               TEXT PRIMARY KEY,
	name             TEXT NOT NULL,
	enabled          INTEGER DEFAULT 1,
	workflow_pattern TEXT NOT NULL DEFAULT '*',
	condition_json   TEXT NOT NULL,
	severity         TEXT NOT NULL DEFAULT 'warning',
	channels         TEXT NOT NULL DEFAULT 'toast',
	cooldown_ms      INTEGER DEFAULT 60000
);

CREATE TABLE IF NOT EXISTS alert_events (
	id            ` + idDef + `,
	rule_id       TEXT NOT NULL,
	rule_name     TEXT NOT NULL,
	severity      TEXT NOT NULL,
	workflow_id   TEXT NOT NULL,
	workflow_name TEXT NOT NULL,
	message       TEXT NOT NULL,
	acknowledged  INTEGER DEFAULT 0,
	ts_ms         BIGINT NOT NULL DEFAULT 0,
	created_at    ` + ts + ` DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_alert_events_ack ON alert_events(acknowledged, ts_ms DESC);
`
}

var sqliteMigrations = []migration{
	{version: 1, sql: schemaV1(sqliteID, "DATETIME")},
	{version: 2, sql: schemaV2(sqliteID, "DATETIME")},
	{version: 3, sql: schemaV3()},
	{version: 4, sql: schemaV4()},
	{version: 5, sql: schemaV5("DATETIME")},
	{version: 6, sql: schemaV6("DATETIME")},
	{version: 7, sql: schemaV7("DATETIME")},
	{version: 8, sql: schemaV8(sqliteID, "DATETIME")},
	{version: 9, sql: schemaV9()},
	{version: 10, sql: schemaV10()},
	{version: 11, sql: schemaV11(sqliteID, "DATETIME")},
	{version: 12, sql: schemaV12("DATETIME")},
	{version: 13, sql: schemaV13(sqliteID, "DATETIME")},
	{version: 14, sql: schemaV14()},
	{version: 15, sql: schemaV15(sqliteID, "DATETIME")},
	{version: 16, sql: schemaV16()},
	{version: 17, sql: schemaV17("DATETIME")},
	{version: 18, sql: schemaV18()},
	{version: 19, sql: schemaV19()},
}

var pgMigrations = []migration{
	{version: 1, sql: schemaV1(pgID, "TIMESTAMPTZ")},
	{version: 2, sql: schemaV2(pgID, "TIMESTAMPTZ")},
	{version: 3, sql: schemaV3()},
	{version: 4, sql: schemaV4()},
	{version: 5, sql: schemaV5("TIMESTAMPTZ")},
	{version: 6, sql: schemaV6("TIMESTAMPTZ")},
	{version: 7, sql: schemaV7("TIMESTAMPTZ")},
	{version: 8, sql: schemaV8(pgID, "TIMESTAMPTZ")},
	{version: 9, sql: schemaV9()},
	{version: 10, sql: schemaV10()},
	{version: 11, sql: schemaV11(pgID, "TIMESTAMPTZ")},
	{version: 12, sql: schemaV12("TIMESTAMPTZ")},
	{version: 13, sql: schemaV13(pgID, "TIMESTAMPTZ")},
	{version: 14, sql: schemaV14()},
	{version: 15, sql: schemaV15(pgID, "TIMESTAMPTZ")},
	{version: 16, sql: schemaV16()},
	{version: 17, sql: schemaV17("TIMESTAMPTZ")},
	{version: 18, sql: schemaV18()},
	{version: 19, sql: schemaV19()},
}

// schemaV3 — ciclo de vida do alerta: como o evento foi tratado pelo operador.
// ” = novo · 'ack' = reconhecido · 'rerun' = job re-executado · 'set_ok' = set OK.
// ALTER idêntico em SQLite e Postgres.
func schemaV3() string {
	return `ALTER TABLE alert_events ADD COLUMN resolution TEXT NOT NULL DEFAULT ''`
}

// schemaV4 — P2/escala: denormaliza a folder (team) na própria instance para
// permitir FILTRO e PAGINAÇÃO server-side por folder (antes só existia na def, o
// que forçava baixar o dia inteiro e filtrar no cliente). Os índices cobrem o
// filtro por folder e a paginação por cursor (order_date, scheduled_at, id).
// ALTER idêntico em SQLite e Postgres; instances antigas ficam com team=” (órfãs,
// já escondidas de não-admin) — novas dailies preenchem. Ver instances.go.
func schemaV4() string {
	return `ALTER TABLE instances ADD COLUMN team TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_instances_team ON instances(order_date, team);
CREATE INDEX IF NOT EXISTS idx_instances_page ON instances(order_date, scheduled_at, id)`
}

// schemaV5 — ciclo de vida da daily (carry-over entre diárias, Control-M-like).
// Uma instance que sobrevive à virada AVANÇA seu order_date para o novo dia (mesmo
// ID/status/started_at/snapshot/eventos), em vez de ser duplicada — ver
// scheduler.carryOver. As 3 colunas guardam a contabilidade do carry:
//   - carry_budget: diárias EXTRA restantes p/ um job não-OK (NOTOK/keepActive).
//     -1 = ainda não avaliado (lazy-init na 1ª virada); 0 = esgotado (não carrega).
//     RUNNING/HELD carregam incondicionalmente e não consomem orçamento.
//   - carried_from: order_date de ORIGEM (1ª diária da ordem), p/ exibir "desde X".
//   - carried_at: instante da última virada; RE-ARMA o watchdog de stuck-running
//     (um RUNNING carregado com started_at antigo não é reapado no instante em que
//     aparece no novo dia — staleness medido de max(started_at, carried_at)).
//
// ALTER idêntico em SQLite e Postgres; instances antigas ficam com defaults.
func schemaV5(ts string) string {
	return `ALTER TABLE instances ADD COLUMN carry_budget INTEGER NOT NULL DEFAULT -1;
ALTER TABLE instances ADD COLUMN carried_from TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN carried_at ` + ts
}

// schemaV6 — frota de agentes: a tabela `agents` já existe desde a v1 (id ·
// capabilities · last_seen_at · online), mas era inerte. Aqui ela ENRIQUECE para
// alimentar a tela de Agentes: metadata (OS/arch/host/versão), início do processo
// (started_at → uptime), instante da conexão e first_seen. O "online agora" é a
// verdade do hub (in-memory); a tabela guarda metadata + last_seen_at pra sobreviver
// à desconexão e ao restart. ALTER ADD COLUMN nullable (sem default não-constante,
// p/ compat SQLite); first_seen/last_seen_at são preenchidos no upsert (ver agents.go).
func schemaV6(ts string) string {
	return `ALTER TABLE agents ADD COLUMN os TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN arch TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN host TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN version TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN started_at ` + ts + `;
ALTER TABLE agents ADD COLUMN connected_at ` + ts + `;
ALTER TABLE agents ADD COLUMN first_seen ` + ts + ``
}

// schemaV7 — Actions / On-Do (Control-M On/Do). Ledger de disparos de ação para
// IDEMPOTÊNCIA: cada regra (chave = índice da regra no array do job) dispara no
// máximo uma vez por instance. PK (instance_id, action_key) garante isso mesmo
// entre ticks e através de restart (a tabela é durável, não in-memory). O motor
// faz claim com check-then-insert (tick roda só no líder, single-threaded; a
// transição terminal de uma instance é única) — a PK é a rede de segurança.
// Portável SQLite/PG. Ver scheduler/actions.go.
func schemaV7(ts string) string {
	return `CREATE TABLE IF NOT EXISTS action_fires (
	instance_id TEXT NOT NULL,
	action_key  TEXT NOT NULL,
	fired_at    ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (instance_id, action_key)
)`
}

// schemaV8 — Aprofundamento Control-M (2026-07-03): cyclic runtime + CONFIRM +
// ViewPoints salvos.
//   - cycle_runs: execuções OK completadas de um job cyclic (a MESMA instance
//     re-arma pra WAITING a cada IntervalMin dentro da janela — ver maybeCycle).
//   - confirmed: Control-M "Wait for confirmation" — def com confirm:true só é
//     reivindicada depois do Confirm do operador (gate WAIT_CONFIRM).
//   - viewpoints: filtros nomeados do Monitoring (por usuário; shared=1 visível
//     a todos), base dos dashboards prontos.
//
// ALTERs idênticos em SQLite e Postgres; instances antigas ficam com defaults.
func schemaV8(idDef, ts string) string {
	return `ALTER TABLE instances ADD COLUMN cycle_runs INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN confirmed INTEGER NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS viewpoints (
	id           ` + idDef + `,
	owner        TEXT NOT NULL,
	name         TEXT NOT NULL,
	filters_json TEXT NOT NULL DEFAULT '{}',
	shared       INTEGER NOT NULL DEFAULT 0,
	created_at   ` + ts + ` DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_viewpoints_owner ON viewpoints(owner)`
}

// schemaV9 — dry_run SNAPSHOTADO na instance (2026-07-04). Igual ao `team` (v4), a
// flag dryRun ("job roda sem fazer nada — log only") passa a ser CONGELADA na
// instância no momento da ordem, em vez de lida da definition viva.
//
// Por quê: um job já materializado no Monitoring é IMUTÁVEL — só muda numa NOVA
// ordem (daily/force/manual). Se o selo 👻GHOST fosse derivado da def viva, ligar
// dryRun no Design e publicar reescreveria o card de um job já ordenado (o "ghost
// fantasma" que não devia existir). Congelando aqui, o Monitoring reflete o dia
// COMO FOI SCHEDULADO; a mudança no Design só aparece na próxima ordem.
//
// ALTER idêntico em SQLite e Postgres; instances antigas ficam com dry_run=0
// (default seguro: sem selo até a próxima daily reescrever com o valor real).
func schemaV9() string {
	return `ALTER TABLE instances ADD COLUMN dry_run INTEGER NOT NULL DEFAULT 0`
}

// schemaV10 — CTM-1 (2026-07-06): variáveis LOCAIS da instance (%%SETLOCAL).
// JSON {nome: valor} gravado pelo scheduler ao término de cada tentativa e lido
// na interpolação dos params da MESMA instance (retries/reruns/voltas cyclic).
// Escopo morre com a instance — nunca vaza pro VariableStore global.
// ALTER idêntico em SQLite e Postgres; instances antigas ficam com ” (sem vars).
func schemaV10() string {
	return `ALTER TABLE instances ADD COLUMN local_vars TEXT NOT NULL DEFAULT ''`
}

// schemaV11 — E2 (2026-07-07): trilha de auditoria de segurança PERSISTIDA.
// O pkg audit (SIEM) era transporte puro (JSON em stderr + POST opcional);
// auditoria enterprise precisa de trilha durável — retenção configurável
// (audit_retention_days, ver scheduler/auditgc.go) e export JSONL paginado
// (GET /api/audit/export). Todo s.audit() da API (login, definition.write,
// settings.write) passa a gravar aqui além de emitir pro sink.
func schemaV11(idDef, ts string) string {
	return `CREATE TABLE IF NOT EXISTS audit_events (
	id      ` + idDef + `,
	ts      ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	kind    TEXT NOT NULL,
	actor   TEXT NOT NULL DEFAULT '',
	action  TEXT NOT NULL DEFAULT '',
	target  TEXT NOT NULL DEFAULT '',
	outcome TEXT NOT NULL DEFAULT '',
	ip      TEXT NOT NULL DEFAULT '',
	detail  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_events_ts ON audit_events(ts)`
}

// schemaV12 — E5 (2026-07-07): relatório/SLO da daily. `report_sent_at` marca
// que o push do relatório do dia (setting daily_report_channels) já foi enviado
// — o claim é um UPDATE ... WHERE report_sent_at IS NULL, então o envio é
// idempotente mesmo com múltiplos nós/ticks. ALTER idêntico SQLite/PG.
func schemaV12(ts string) string {
	return `ALTER TABLE daily_runs ADD COLUMN report_sent_at ` + ts
}

// schemaV13 — Diferenciais leva 2 (2026-07-07).
//
// external_events (D-3, event-driven confiável): ingestão idempotente de eventos
// EXTERNOS (webhook de outro sistema → condition/force). O `id` vem do EMISSOR
// (dedupe key): o INSERT com PK é o teste de duplicata — retry do emissor não
// re-aplica o efeito. `applied` registra o que o evento causou (forense).
//
// job_templates (D-13): templates reutilizáveis de job — metadado operacional
// (não versionado no workspace; a def criada a partir dele, sim).
func schemaV13(idDef, ts string) string {
	_ = idDef
	return `CREATE TABLE IF NOT EXISTS external_events (
	id          TEXT PRIMARY KEY,
	source      TEXT NOT NULL DEFAULT '',
	kind        TEXT NOT NULL DEFAULT '',
	payload     TEXT NOT NULL DEFAULT '',
	applied     TEXT NOT NULL DEFAULT '',
	received_at ` + ts + ` DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS job_templates (
	name        TEXT PRIMARY KEY,
	description TEXT NOT NULL DEFAULT '',
	definition  TEXT NOT NULL,
	created_by  TEXT NOT NULL DEFAULT '',
	created_at  ` + ts + ` DEFAULT CURRENT_TIMESTAMP
)`
}

// schemaV14 — hold_scope: ORIGEM do HOLD, para separar "pausa de folder" (D-2) de
// um hold individual de operador. Ambos usam o MESMO status HELD; só o escopo
// diferencia quem pode liberar:
//   - ''       = hold individual (ou instância não-HELD): pode ser liberado 1-a-1
//                pelo Release do operador.
//   - 'folder' = segurado por uma PAUSA DE FOLDER (POST /folders/{name}/pause):
//                não pode ser liberado individualmente — só o resume da folder
//                inteira destrava (Control-M "Hold folder" ⇒ "Release folder").
//
// Sinaliza na UI: o cadeado do card/linha muda de tinta (folder vs individual) e
// a própria folder ganha um cadeado quando tem qualquer job em hold de folder.
// ALTER idêntico em SQLite e Postgres; instances antigas ficam com '' (hold
// legado conta como individual — comportamento anterior preservado).
func schemaV14() string {
	return `ALTER TABLE instances ADD COLUMN hold_scope TEXT NOT NULL DEFAULT ''`
}

// schemaV15 — eventos de dependência com CONSUMO por instância (2026-07-13).
//
// ⚠️ APOSENTADO (2026-07-17): as tabelas dep_events/dep_claims ficaram órfãs —
// a unificação de dependências no POOL de condições (domain/conditions.go)
// removeu todo o runtime de eventos/claims. As tabelas permanecem no schema
// por compat de migração (bancos existentes), mas nada mais escreve ou lê.
//
// Motivação (report do usuário): a satisfação de uma dependência era derivada do
// status VIVO do upstream — rerun/cancel do pai "apagava" linhas já satisfeitas
// no Monitoring, e uma CÓPIA forçada do filho nascia com a condição já aceita
// (o mesmo término do pai satisfazia duas instances). Novo modelo:
//
//   - dep_events: cada término TERMINAL (OK/NOTOK pós-retries, Set OK) de uma
//     instance publica um EVENTO imutável. Rerun do pai = evento NOVO.
//   - dep_claims: a satisfação de uma aresta é um CLAIM (latch) do consumidor
//     sobre um evento. UNIQUE(event_id, consumer_def_id) = um evento não pode
//     satisfazer duas instances da MESMA definition (a cópia forçada espera um
//     término novo); UNIQUE(consumer_instance_id, upstream_def_id) = um claim
//     por aresta por consumidor. Rerun do CONSUMIDOR após OK vira LÁPIDE
//     (consumer_instance_id ganha sufixo '#spent@<event>': a linha do novo run
//     reseta mas o evento segue GASTO — WAIT EVENT até término novo do pai);
//     rerun de não-consumido devolve os eventos pro pool; rerun/cancel do PAI
//     não toca claims já feitos (a linha satisfeita permanece verde).
//
// instances.force_mode distingue o Force: '' = "Run Now" clássico (bypass total
// de gates, ordem EXISTENTE); 'order' = "Order Force" do Design (ordem NOVA fora
// do agendamento, mas que RESPEITA os gates de runtime — deps/conditions/agente).
func schemaV15(idDef, ts string) string {
	return `
CREATE TABLE IF NOT EXISTS dep_events (
	id          ` + idDef + `,
	def_id      TEXT NOT NULL,
	instance_id TEXT NOT NULL,
	order_date  TEXT NOT NULL,
	status      TEXT NOT NULL,
	created_at  ` + ts + ` DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_dep_events_def_date ON dep_events(def_id, order_date);
CREATE INDEX IF NOT EXISTS idx_dep_events_instance ON dep_events(instance_id);

CREATE TABLE IF NOT EXISTS dep_claims (
	event_id             BIGINT NOT NULL,
	consumer_instance_id TEXT NOT NULL,
	consumer_def_id      TEXT NOT NULL,
	upstream_def_id      TEXT NOT NULL,
	claimed_at           ` + ts + ` DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(event_id, consumer_def_id),
	UNIQUE(consumer_instance_id, upstream_def_id)
);
CREATE INDEX IF NOT EXISTS idx_dep_claims_consumer ON dep_claims(consumer_instance_id);

ALTER TABLE instances ADD COLUMN force_mode TEXT NOT NULL DEFAULT ''`
}

// schemaV17 — meta_flags: marcadores de migrações one-time que precisam de
// LÓGICA Go (não só SQL) — ex.: o backfill do pool de condições na unificação
// de dependências (scheduler.MigrateConditionsUnify, 2026-07-17). Cada rotina
// checa/insere seu nome aqui para rodar exatamente uma vez por banco.
func schemaV17(ts string) string {
	return `CREATE TABLE IF NOT EXISTS meta_flags (
	name    TEXT PRIMARY KEY,
	done_at ` + ts + ` DEFAULT CURRENT_TIMESTAMP
)`
}

// schemaV18 — M1: imutabilidade TOTAL do Monitoring (2026-07-17). Tudo que o
// card/lista/grafo exibem passa a vir CONGELADO na instance (mesmo racional do
// dry_run v9 e do team v4, agora completo):
//   - label / job_type: nome e tipo exibidos — renomear/trocar tipo no Design
//     não pode reescrever cards já ordenados (report do usuário).
//   - confirm_req: o gate visual WAIT CONFIRM (def.confirm da ordem).
//   - environment / pinned_agent: entradas do WAIT AGENT (roteamento congelado).
//   - conds_in / conds_out_add: JSON arrays (strings com sufixo @odat/@prev/@stat)
//     das condições da ordem — as LINHAS do grafo do Monitoring derivam daqui,
//     nunca mais da topologia viva do Design (criar job novo no dia não redesenha
//     instancias antigas). conds_in já gravado EXPANDIDO (ExpandSnapshotConditions
//     cobre upstream legado).
// Backfill em Go (parse do definition_snapshot) roda no boot via meta_flags —
// ver scheduler.MigrateMonitoringSnapshot. Instances antigas sem snapshot ficam
// com defaults ('' — o front cai na def viva SÓ nesse caso legado).
// ALTERs idênticos em SQLite e Postgres.
func schemaV18() string {
	return `ALTER TABLE instances ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN job_type TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN confirm_req INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN environment TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN pinned_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN conds_in TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN conds_out_add TEXT NOT NULL DEFAULT ''`
}

// schemaV19 — recursos/quotas (F15) CONGELADOS na instance (2026-07-18). Mesmo
// racional dos campos M1 (v18): o card "WAIT RESOURCE" do Monitoring deriva do
// que a ORDEM exigia, não da def viva — mudar os recursos de um job no Design
// não reescreve cards já ordenados. JSON {nome: qtd}; '' = sem recurso. O gate
// do scheduler já lê os recursos do definition_snapshot (defForInstance), então
// esta coluna só PROMOVE o mesmo dado pra lista sem parsear o snapshot por linha.
// Backfill em Go (parse do snapshot) roda no boot via meta_flags (v19).
// ALTER idêntico em SQLite e Postgres; instances antigas ficam com '' (o card
// cai no fallback: sem recurso conhecido, sem selo).
func schemaV19() string {
	return `ALTER TABLE instances ADD COLUMN resources TEXT NOT NULL DEFAULT ''`
}

// schemaV16 — held_from_status: o status que a instance tinha ao entrar em HOLD
// (2026-07-16, "hold geral"). O Hold — individual, bulk ou pausa de folder —
// passou a valer para QUALQUER status não-RUNNING (não só WAITING): segurar um
// NOTOK congela o tratamento, segurar um OK evita rerun acidental, e a pausa de
// folder segura o dia INTEIRO (incluindo carry-over). O Release/Resume restaura
// o status ORIGINAL daqui em vez de mandar tudo pra WAITING (que re-executaria
// um OK segurado). '' = hold legado/pré-migração: release cai em WAITING, o
// comportamento antigo. Só tem significado enquanto status='HELD' — todo hold
// sobrescreve o valor na entrada.
func schemaV16() string {
	return `ALTER TABLE instances ADD COLUMN held_from_status TEXT NOT NULL DEFAULT ''`
}
