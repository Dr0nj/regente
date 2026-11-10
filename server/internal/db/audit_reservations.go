package db

import (
	"fmt"
	"github.com/Dr0nj/regente-server/internal/telemetry"
)

// Limite por registro: reservas cobrem quantidade e o pior caso em bytes.
const MaxAuditPayloadBytes = 64 << 10

var auditExecutionSlots = []string{"dispatch", "accepted", "started", "terminal", "cancel", "cancel_delivery", "recovery"}

func schemaV33(dialect Dialect) string {
	base := `CREATE TABLE audit_reservations (execution_id TEXT NOT NULL REFERENCES execution_attempts(execution_id), slot TEXT NOT NULL, PRIMARY KEY(execution_id,slot));
CREATE INDEX audit_delivery_pending_category ON audit_delivery(acknowledged,category,seq);
INSERT INTO audit_reservations(execution_id,slot)
SELECT a.execution_id,s.slot FROM execution_attempts a CROSS JOIN
(SELECT 'dispatch' AS slot UNION ALL SELECT 'accepted' UNION ALL SELECT 'started' UNION ALL SELECT 'terminal' UNION ALL SELECT 'cancel' UNION ALL SELECT 'cancel_delivery' UNION ALL SELECT 'recovery') s
WHERE a.state NOT IN ('succeeded','failed','cancelled');`
	return base + schedulerChangeSchema(dialect)
}

// O lock de audit_head já serializa a admissão em SQLite e Postgres.
// Migração pode começar acima do teto: novos trabalhos ficam bloqueados enquanto
// as reservas inventariadas permitem concluir os trabalhos anteriores, sem apagar trilha.
func (t *Tx) ReserveExecutionAudit(id string) error {
	if t.audit == nil {
		return nil
	}
	var pending, reserved int64
	if err := t.Tx.QueryRow("SELECT COUNT(*) FROM audit_delivery WHERE acknowledged=0 AND category='security'").Scan(&pending); err != nil {
		return err
	}
	if err := t.Tx.QueryRow("SELECT COUNT(*) FROM audit_reservations").Scan(&reserved); err != nil {
		return err
	}
	if pending+reserved+int64(len(auditExecutionSlots))+1 > t.audit.capacity {
		telemetry.Count(telemetry.AuditRejected, 1)
		return ErrAuditFull
	}
	for _, slot := range auditExecutionSlots {
		if _, err := t.Tx.Exec(rebind("INSERT INTO audit_reservations(execution_id,slot) VALUES(?,?)", t.dialect), id, slot); err != nil {
			return err
		}
	}
	return nil
}

// Crédito é consumido atomicamente com estado+evidência. Rollback restaura a reserva.
// Uma redelivery sem crédito continua sujeita ao teto geral; não cria orçamento.
func (t *Tx) UseExecutionAudit(id, slot string, terminal bool) error {
	if t.audit == nil {
		return nil
	}
	if t.auditCredit {
		return fmt.Errorf("audit transaction already has a reservation")
	}
	result, err := t.Tx.Exec(rebind("DELETE FROM audit_reservations WHERE execution_id=? AND slot=?", t.dialect), id, slot)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	t.auditCredit = n == 1
	if terminal {
		_, err = t.Tx.Exec(rebind("DELETE FROM audit_reservations WHERE execution_id=?", t.dialect), id)
	}
	return err
}

func schedulerChangeSchema(dialect Dialect) string {
	base := `CREATE TABLE scheduler_changes(instance_id TEXT PRIMARY KEY,version BIGINT NOT NULL DEFAULT 1,changed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX scheduler_changes_fifo ON scheduler_changes(changed_at,instance_id);
CREATE INDEX instances_reconciliation ON instances(order_date,id);`
	columns := "status,scheduled_at,forced,force_mode,confirmed,definition_snapshot,order_date,carried_from,held_from_status,started_at,carried_at"
	if dialect == Postgres {
		return base + `
CREATE FUNCTION regente_scheduler_change() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  INSERT INTO scheduler_changes(instance_id) VALUES(OLD.id) ON CONFLICT(instance_id) DO UPDATE SET version=scheduler_changes.version+1;
 ELSE
  INSERT INTO scheduler_changes(instance_id) VALUES(NEW.id) ON CONFLICT(instance_id) DO UPDATE SET version=scheduler_changes.version+1;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER scheduler_instance_insert AFTER INSERT ON instances FOR EACH ROW EXECUTE FUNCTION regente_scheduler_change();
CREATE TRIGGER scheduler_instance_update AFTER UPDATE OF ` + columns + ` ON instances FOR EACH ROW EXECUTE FUNCTION regente_scheduler_change();
CREATE TRIGGER scheduler_instance_delete AFTER DELETE ON instances FOR EACH ROW EXECUTE FUNCTION regente_scheduler_change();`
	}
	for _, item := range []struct{ name, event, ref string }{{"insert", "INSERT", "NEW"}, {"update", "UPDATE OF " + columns, "NEW"}, {"delete", "DELETE", "OLD"}} {
		base += "CREATE TRIGGER scheduler_instance_" + item.name + " AFTER " + item.event + " ON instances BEGIN INSERT INTO scheduler_changes(instance_id) VALUES(" + item.ref + ".id) ON CONFLICT(instance_id) DO UPDATE SET version=version+1; END;"
	}
	return base
}
