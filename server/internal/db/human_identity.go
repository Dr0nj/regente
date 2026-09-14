package db

// schemaV25 preserva contas; sessões antigas não provam origem nem vínculo OIDC.
func schemaV25(ts string) string {
	return `
DELETE FROM sessions;
ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN requires_link INTEGER NOT NULL DEFAULT 0;
UPDATE users SET requires_link=1 WHERE password_hash='!federated';
ALTER TABLE sessions ADD COLUMN source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE sessions ADD COLUMN browser INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN csrf TEXT NOT NULL DEFAULT '';
CREATE TABLE external_identities (
 issuer TEXT NOT NULL, subject TEXT NOT NULL,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 PRIMARY KEY(issuer, subject)
);
CREATE TABLE auth_transactions (
 state_hash TEXT PRIMARY KEY, nonce TEXT NOT NULL, verifier TEXT NOT NULL,
 expires_at ` + ts + ` NOT NULL
);
CREATE TABLE web_tickets (
 token_hash TEXT PRIMARY KEY, session_token TEXT NOT NULL,
 expires_at ` + ts + ` NOT NULL
);
CREATE TABLE identity_audit (
 event_id TEXT PRIMARY KEY, actor TEXT NOT NULL, action TEXT NOT NULL,
 user_id BIGINT NOT NULL, issuer TEXT NOT NULL DEFAULT '', subject TEXT NOT NULL DEFAULT '',
 created_at ` + ts + ` NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`
}
