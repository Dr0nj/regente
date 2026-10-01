package scheduler

import (
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/domain"
)

// Ausência legada é diferente de payload presente porém corrompido.
func snapshotError(r instRow) string {
	if r.Snapshot == "" {
		if r.SnapshotChecksum != "" {
			return "Order snapshot is missing from a verified daily order; restore verified state or reorder."
		}
		return ""
	}
	if r.SnapshotChecksum != "" && digest(r.Snapshot) != r.SnapshotChecksum {
		return "Order snapshot checksum differs from the frozen daily plan; restore verified state or reorder."
	}
	var d domain.JobDefinition
	if json.Unmarshal([]byte(r.Snapshot), &d) != nil || d.ID == "" || d.ID != r.DefID {
		return "Order snapshot is invalid; restore verified state or reorder. Live definitions are never used for a corrupt snapshot."
	}
	if d.BusinessTime != nil && d.BusinessTime.Validate() != nil {
		return "Order snapshot has an invalid business calendar; restore verified state or reorder."
	}
	return ""
}
func integrityBlock(r instRow) string {
	if reason := snapshotError(r); reason != "" {
		return reason
	}
	if r.DailyState != "" && r.DailyState != "completed" && r.DailyState != "legacy" {
		return fmt.Sprintf("Daily materialization is %s; resume the frozen daily before executing this order.", r.DailyState)
	}
	return ""
}

// OrderSnapshotError compartilha diagnóstico com a API sem expor a definição viva.
func OrderSnapshotError(defID, raw, checksum string) string {
	return snapshotError(instRow{DefID: defID, Snapshot: raw, SnapshotChecksum: checksum})
}

// Consulta também o ledger para distinguir ausência legada de perda do snapshot verificado.
func (s *Scheduler) orderIntegrity(id string) (instRow, error) {
	var r instRow
	err := s.db.QueryRow(
		`SELECT i.definition_id,COALESCE(i.definition_snapshot,''),COALESCE(l.snapshot_checksum,''),COALESCE(d.state,'')
   FROM instances i LEFT JOIN daily_order_ledger l ON l.instance_id=i.id
   LEFT JOIN daily_runs d ON d.order_date=l.order_date WHERE i.id=?`, id,
	).Scan(&r.DefID, &r.Snapshot, &r.SnapshotChecksum, &r.DailyState)
	return r, err
}
