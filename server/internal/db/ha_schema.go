package db

// I11: identidade da reserva e termo de liderança; reservas sem vínculo ficam
// conservadas para diagnóstico, nunca são silenciosamente descartadas.
func schemaV29() string {
	return `
ALTER TABLE execution_resource_holds ADD COLUMN execution_id TEXT NOT NULL DEFAULT '';
UPDATE execution_resource_holds SET execution_id=COALESCE((SELECT o.current_execution FROM runtime_orders r JOIN lab_orders o ON o.id=r.order_id WHERE r.instance_id=execution_resource_holds.instance_id),'');
CREATE INDEX idx_resource_execution ON execution_resource_holds(execution_id);
CREATE TABLE scheduler_leadership(lock_key BIGINT PRIMARY KEY,epoch BIGINT NOT NULL,backend_pid INTEGER NOT NULL);
CREATE TABLE execution_agent_capacity(agent_id TEXT PRIMARY KEY REFERENCES machine_principals(agent_id),slots INTEGER NOT NULL,pending_limit INTEGER NOT NULL,available INTEGER NOT NULL,last_seen BIGINT NOT NULL);
`
}
