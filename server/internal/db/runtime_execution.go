package db

func schemaV28() string {
	return `
ALTER TABLE lab_orders ADD COLUMN runtime INTEGER NOT NULL DEFAULT 0;
CREATE TABLE runtime_orders(instance_id TEXT PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,order_id TEXT NOT NULL UNIQUE REFERENCES lab_orders(id));
ALTER TABLE machine_principals ADD COLUMN internal INTEGER NOT NULL DEFAULT 0;
ALTER TABLE execution_attempts ADD COLUMN last_contact BIGINT NOT NULL DEFAULT 0;
ALTER TABLE execution_attempts ADD COLUMN reason TEXT NOT NULL DEFAULT '';
ALTER TABLE execution_attempts ADD COLUMN resolved_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE execution_attempts ADD COLUMN resolved_by TEXT NOT NULL DEFAULT '';
CREATE TABLE execution_effects(
 id TEXT PRIMARY KEY,execution_id TEXT NOT NULL REFERENCES execution_attempts(execution_id),
 instance_id TEXT NOT NULL,kind TEXT NOT NULL,payload TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending',created_at BIGINT NOT NULL,lease_until BIGINT NOT NULL DEFAULT 0,generation INTEGER NOT NULL DEFAULT 0,
 reason TEXT NOT NULL DEFAULT '',resolved_by TEXT NOT NULL DEFAULT '',resolved_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_execution_effects_pending ON execution_effects(state,created_at);
CREATE TABLE execution_effect_decisions(request_key TEXT PRIMARY KEY,effect_id TEXT NOT NULL REFERENCES execution_effects(id),actor TEXT NOT NULL,decision TEXT NOT NULL,reason TEXT NOT NULL,classification TEXT NOT NULL,generation INTEGER NOT NULL,created_at BIGINT NOT NULL);
CREATE TABLE execution_resource_holds(instance_id TEXT NOT NULL,name TEXT NOT NULL,quantity INTEGER NOT NULL,PRIMARY KEY(instance_id,name));
CREATE TABLE execution_decisions(
 request_key TEXT PRIMARY KEY,execution_id TEXT NOT NULL REFERENCES execution_attempts(execution_id),
 actor TEXT NOT NULL,decision TEXT NOT NULL,reason TEXT NOT NULL,created_at BIGINT NOT NULL
);
`
}
