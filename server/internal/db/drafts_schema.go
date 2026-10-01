package db

// I12: payload portátil e histórico CAS; legado continua revision=0 até verificação.
func schemaV30() string {
	return `
ALTER TABLE design_sessions ADD COLUMN revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE design_sessions ADD COLUMN state TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE design_sessions ADD COLUMN dirty INTEGER NOT NULL DEFAULT 1;
ALTER TABLE design_sessions ADD COLUMN publication TEXT NOT NULL DEFAULT '';
CREATE TABLE design_draft_versions (
 session_id TEXT NOT NULL REFERENCES design_sessions(id) ON DELETE CASCADE,
 revision BIGINT NOT NULL, payload TEXT NOT NULL, checksum TEXT NOT NULL,
 PRIMARY KEY(session_id, revision)
);
CREATE TABLE design_draft_audit (
 event_id TEXT PRIMARY KEY, session_id TEXT NOT NULL, revision BIGINT NOT NULL,
 actor TEXT NOT NULL, operation TEXT NOT NULL, checksum TEXT NOT NULL,
 at_ms BIGINT NOT NULL
);
CREATE INDEX idx_design_draft_audit_session ON design_draft_audit(session_id, at_ms);
CREATE TABLE design_draft_publications (
 session_id TEXT PRIMARY KEY, owner TEXT NOT NULL, revision BIGINT NOT NULL,
 folders_json TEXT NOT NULL, result_json TEXT NOT NULL, at_ms BIGINT NOT NULL
);
`
}
