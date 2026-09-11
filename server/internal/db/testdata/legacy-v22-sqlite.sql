CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS instances (
	id            TEXT PRIMARY KEY,
	definition_id TEXT NOT NULL,
	order_date    TEXT NOT NULL,
	status        TEXT NOT NULL,
	scheduled_at  DATETIME NOT NULL,
	started_at    DATETIME,
	finished_at   DATETIME,
	agent_id      TEXT,
	exit_code     INTEGER,
	output        TEXT,
	forced        INTEGER DEFAULT 0,
	created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
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
	last_seen_at DATETIME NOT NULL,
	online       INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS daily_runs (
	order_date  TEXT PRIMARY KEY,
	started_at  DATETIME NOT NULL,
	finished_at DATETIME
);
CREATE TABLE IF NOT EXISTS instance_events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	instance_id TEXT NOT NULL,
	ts          DATETIME DEFAULT CURRENT_TIMESTAMP,
	kind        TEXT NOT NULL,
	actor       TEXT,
	message     TEXT,
	git_commit_sha TEXT,
	pr_number   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_instance_events_inst ON instance_events(instance_id, ts);
CREATE TABLE IF NOT EXISTS users (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	username       TEXT UNIQUE NOT NULL,
	password_hash  TEXT NOT NULL,
	role           TEXT NOT NULL DEFAULT 'viewer',
	created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
	must_change_pw INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	expires_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE TABLE IF NOT EXISTS folder_acls (
	user_id     INTEGER NOT NULL,
	folder_name TEXT NOT NULL,
	perms       TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (user_id, folder_name)
);
CREATE TABLE IF NOT EXISTS definition_audit (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	ts             DATETIME DEFAULT CURRENT_TIMESTAMP,
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
	set_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
	set_by     TEXT,
	PRIMARY KEY (name, scope_date)
);
CREATE INDEX IF NOT EXISTS idx_conditions_scope ON conditions(scope_date);
CREATE TABLE IF NOT EXISTS sla_breaches (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	instance_id   TEXT NOT NULL,
	definition_id TEXT NOT NULL,
	kind          TEXT NOT NULL,
	severity      TEXT NOT NULL DEFAULT 'warning',
	message       TEXT,
	detected_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
	notified      INTEGER DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sla_inst_kind ON sla_breaches(instance_id, kind);
CREATE INDEX IF NOT EXISTS idx_sla_detected ON sla_breaches(detected_at DESC);
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS agent_tokens (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	token        TEXT UNIQUE NOT NULL,
	label        TEXT NOT NULL DEFAULT '',
	created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
	last_used_at DATETIME
);
CREATE TABLE IF NOT EXISTS variables (
	name       TEXT PRIMARY KEY,
	value      TEXT NOT NULL DEFAULT '',
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_by TEXT
);
CREATE TABLE IF NOT EXISTS design_sessions (
	id               TEXT PRIMARY KEY,
	actor            TEXT NOT NULL,
	folders_json     TEXT NOT NULL DEFAULT '[]',
	new_folders_json TEXT NOT NULL DEFAULT '[]',
	base_sha         TEXT NOT NULL DEFAULT '',
	path             TEXT NOT NULL,
	created_at       DATETIME NOT NULL,
	last_touch       DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_design_sessions_actor ON design_sessions(actor);
INSERT INTO schema_migrations(version) VALUES(1);
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
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	rule_id       TEXT NOT NULL,
	rule_name     TEXT NOT NULL,
	severity      TEXT NOT NULL,
	workflow_id   TEXT NOT NULL,
	workflow_name TEXT NOT NULL,
	message       TEXT NOT NULL,
	acknowledged  INTEGER DEFAULT 0,
	ts_ms         BIGINT NOT NULL DEFAULT 0,
	created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_alert_events_ack ON alert_events(acknowledged, ts_ms DESC);
INSERT INTO schema_migrations(version) VALUES(2);
ALTER TABLE alert_events ADD COLUMN resolution TEXT NOT NULL DEFAULT '';
INSERT INTO schema_migrations(version) VALUES(3);
ALTER TABLE instances ADD COLUMN team TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_instances_team ON instances(order_date, team);
CREATE INDEX IF NOT EXISTS idx_instances_page ON instances(order_date, scheduled_at, id);
INSERT INTO schema_migrations(version) VALUES(4);
ALTER TABLE instances ADD COLUMN carry_budget INTEGER NOT NULL DEFAULT -1;
ALTER TABLE instances ADD COLUMN carried_from TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN carried_at DATETIME;
INSERT INTO schema_migrations(version) VALUES(5);
ALTER TABLE agents ADD COLUMN os TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN arch TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN host TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN version TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN started_at DATETIME;
ALTER TABLE agents ADD COLUMN connected_at DATETIME;
ALTER TABLE agents ADD COLUMN first_seen DATETIME;
INSERT INTO schema_migrations(version) VALUES(6);
CREATE TABLE IF NOT EXISTS action_fires (
	instance_id TEXT NOT NULL,
	action_key  TEXT NOT NULL,
	fired_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (instance_id, action_key)
);
INSERT INTO schema_migrations(version) VALUES(7);
ALTER TABLE instances ADD COLUMN cycle_runs INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN confirmed INTEGER NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS viewpoints (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	owner        TEXT NOT NULL,
	name         TEXT NOT NULL,
	filters_json TEXT NOT NULL DEFAULT '{}',
	shared       INTEGER NOT NULL DEFAULT 0,
	created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_viewpoints_owner ON viewpoints(owner);
INSERT INTO schema_migrations(version) VALUES(8);
ALTER TABLE instances ADD COLUMN dry_run INTEGER NOT NULL DEFAULT 0;
INSERT INTO schema_migrations(version) VALUES(9);
ALTER TABLE instances ADD COLUMN local_vars TEXT NOT NULL DEFAULT '';
INSERT INTO schema_migrations(version) VALUES(10);
CREATE TABLE IF NOT EXISTS audit_events (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	ts      DATETIME DEFAULT CURRENT_TIMESTAMP,
	kind    TEXT NOT NULL,
	actor   TEXT NOT NULL DEFAULT '',
	action  TEXT NOT NULL DEFAULT '',
	target  TEXT NOT NULL DEFAULT '',
	outcome TEXT NOT NULL DEFAULT '',
	ip      TEXT NOT NULL DEFAULT '',
	detail  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_events_ts ON audit_events(ts);
INSERT INTO schema_migrations(version) VALUES(11);
ALTER TABLE daily_runs ADD COLUMN report_sent_at DATETIME;
INSERT INTO schema_migrations(version) VALUES(12);
CREATE TABLE IF NOT EXISTS external_events (
	id          TEXT PRIMARY KEY,
	source      TEXT NOT NULL DEFAULT '',
	kind        TEXT NOT NULL DEFAULT '',
	payload     TEXT NOT NULL DEFAULT '',
	applied     TEXT NOT NULL DEFAULT '',
	received_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS job_templates (
	name        TEXT PRIMARY KEY,
	description TEXT NOT NULL DEFAULT '',
	definition  TEXT NOT NULL,
	created_by  TEXT NOT NULL DEFAULT '',
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO schema_migrations(version) VALUES(13);
ALTER TABLE instances ADD COLUMN hold_scope TEXT NOT NULL DEFAULT '';
INSERT INTO schema_migrations(version) VALUES(14);
CREATE TABLE IF NOT EXISTS dep_events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	def_id      TEXT NOT NULL,
	instance_id TEXT NOT NULL,
	order_date  TEXT NOT NULL,
	status      TEXT NOT NULL,
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_dep_events_def_date ON dep_events(def_id, order_date);
CREATE INDEX IF NOT EXISTS idx_dep_events_instance ON dep_events(instance_id);
CREATE TABLE IF NOT EXISTS dep_claims (
	event_id             BIGINT NOT NULL,
	consumer_instance_id TEXT NOT NULL,
	consumer_def_id      TEXT NOT NULL,
	upstream_def_id      TEXT NOT NULL,
	claimed_at           DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(event_id, consumer_def_id),
	UNIQUE(consumer_instance_id, upstream_def_id)
);
CREATE INDEX IF NOT EXISTS idx_dep_claims_consumer ON dep_claims(consumer_instance_id);
ALTER TABLE instances ADD COLUMN force_mode TEXT NOT NULL DEFAULT '';
INSERT INTO schema_migrations(version) VALUES(15);
ALTER TABLE instances ADD COLUMN held_from_status TEXT NOT NULL DEFAULT '';
INSERT INTO schema_migrations(version) VALUES(16);
CREATE TABLE IF NOT EXISTS meta_flags (
	name    TEXT PRIMARY KEY,
	done_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO schema_migrations(version) VALUES(17);
ALTER TABLE instances ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN job_type TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN confirm_req INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN environment TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN pinned_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN conds_in TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN conds_out_add TEXT NOT NULL DEFAULT '';
INSERT INTO schema_migrations(version) VALUES(18);
ALTER TABLE instances ADD COLUMN resources TEXT NOT NULL DEFAULT '';
INSERT INTO schema_migrations(version) VALUES(19);
CREATE TABLE IF NOT EXISTS resources (
	name     TEXT PRIMARY KEY,
	capacity INTEGER NOT NULL DEFAULT 0
);
INSERT INTO schema_migrations(version) VALUES(20);
ALTER TABLE instances ADD COLUMN cond_logic TEXT NOT NULL DEFAULT '';
INSERT INTO schema_migrations(version) VALUES(21);
CREATE TABLE IF NOT EXISTS instance_output (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	instance_id TEXT NOT NULL,
	attempt     INTEGER NOT NULL DEFAULT 1,
	ts          DATETIME DEFAULT CURRENT_TIMESTAMP,
	chunk       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_instance_output_inst ON instance_output(instance_id, attempt, id);
INSERT INTO schema_migrations(version) VALUES(22);
