package db

// O segredo legado é aposentado, não vinculado por inferência a um host.
// Backups anteriores ainda contêm os segredos antigos: o runbook exige proteção.
func schemaV24() string {
	return `
CREATE TABLE machine_principals (
 agent_id TEXT PRIMARY KEY,
 environment TEXT NOT NULL,
 capabilities TEXT NOT NULL
);
ALTER TABLE agent_tokens RENAME COLUMN token TO token_hash;
UPDATE agent_tokens SET token_hash='retired:' || CAST(id AS TEXT);
ALTER TABLE agent_tokens ADD COLUMN agent_id TEXT REFERENCES machine_principals(agent_id);
ALTER TABLE agent_tokens ADD COLUMN token_prefix TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tokens ADD COLUMN expires_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_tokens ADD COLUMN revoked_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_tokens ADD COLUMN rotated_at BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_agent_tokens_principal ON agent_tokens(agent_id);
`
}
