package db

// schemaV26 não inventa prova para daily antiga: legacy conserva a marca histórica.
// O ledger distingue daily de Force Order e sobrevive a Delete da instance.
func schemaV26() string {
	return `
ALTER TABLE daily_runs ADD COLUMN state TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE daily_runs ADD COLUMN target_commit_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE daily_runs ADD COLUMN expected_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_runs ADD COLUMN inserted_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_runs ADD COLUMN checkpoint INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_runs ADD COLUMN carried_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_runs ADD COLUMN carry_done INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_runs ADD COLUMN plan_json TEXT NOT NULL DEFAULT '';
ALTER TABLE daily_runs ADD COLUMN plan_checksum TEXT NOT NULL DEFAULT '';
ALTER TABLE daily_runs ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
CREATE TABLE daily_order_ledger (
 order_date TEXT NOT NULL REFERENCES daily_runs(order_date) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL,
 definition_id TEXT NOT NULL,
 instance_id TEXT NOT NULL UNIQUE,
 snapshot_checksum TEXT NOT NULL,
 PRIMARY KEY(order_date,ordinal), UNIQUE(order_date,definition_id)
);
CREATE INDEX idx_daily_runs_state ON daily_runs(state,order_date);
`
}
