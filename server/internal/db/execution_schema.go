package db

// I08: ordens de laboratório isoladas; nenhuma tentativa legada recebe identidade inventada.
func schemaV27() string {
	return `
CREATE TABLE execution_queue_lock (id INTEGER PRIMARY KEY, marker INTEGER NOT NULL);
INSERT INTO execution_queue_lock(id,marker) VALUES(1,1);
CREATE TABLE lab_orders (
 id TEXT PRIMARY KEY, source_instance_id TEXT NOT NULL, request_key TEXT NOT NULL UNIQUE,
 snapshot TEXT NOT NULL, snapshot_checksum TEXT NOT NULL, environment TEXT NOT NULL,
 state TEXT NOT NULL, current_execution TEXT NOT NULL DEFAULT '', attempt_number INTEGER NOT NULL DEFAULT 0,
 fence BIGINT NOT NULL DEFAULT 0, created_at BIGINT NOT NULL
);
CREATE TABLE execution_attempts (
 execution_id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES lab_orders(id),
 attempt INTEGER NOT NULL, fence BIGINT NOT NULL, agent_id TEXT NOT NULL REFERENCES machine_principals(agent_id),
 request_key TEXT NOT NULL, intent TEXT NOT NULL, state TEXT NOT NULL,
 lease_until BIGINT NOT NULL DEFAULT 0, created_at BIGINT NOT NULL, accepted_at BIGINT NOT NULL DEFAULT 0,
 started_at BIGINT NOT NULL DEFAULT 0, finished_at BIGINT NOT NULL DEFAULT 0,
 exit_code INTEGER, result_output TEXT NOT NULL DEFAULT '', result_checksum TEXT NOT NULL DEFAULT '',
 output_bytes BIGINT NOT NULL DEFAULT 0, last_output_seq BIGINT NOT NULL DEFAULT 0,
 UNIQUE(order_id,attempt), UNIQUE(order_id,request_key), UNIQUE(order_id,fence)
);
CREATE TABLE execution_outbox (
 id TEXT PRIMARY KEY, execution_id TEXT NOT NULL REFERENCES execution_attempts(execution_id),
 kind TEXT NOT NULL, state TEXT NOT NULL, payload TEXT NOT NULL,
 deliveries INTEGER NOT NULL DEFAULT 0, lease_until BIGINT NOT NULL DEFAULT 0,
 next_at BIGINT NOT NULL DEFAULT 0, created_at BIGINT NOT NULL, UNIQUE(execution_id,kind)
);
CREATE INDEX idx_execution_outbox_claim ON execution_outbox(state,next_at,lease_until);
CREATE TABLE execution_output (
 execution_id TEXT NOT NULL REFERENCES execution_attempts(execution_id), seq BIGINT NOT NULL,
 chunk TEXT NOT NULL, checksum TEXT NOT NULL, created_at BIGINT NOT NULL,
 PRIMARY KEY(execution_id,seq)
);
CREATE TABLE execution_events (
 execution_id TEXT NOT NULL REFERENCES execution_attempts(execution_id), kind TEXT NOT NULL,
 created_at BIGINT NOT NULL, PRIMARY KEY(execution_id,kind)
);
`
}
