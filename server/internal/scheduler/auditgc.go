// E2 — Auditoria enterprise: retenção do event log.
//
// GC diário de instance_events e audit_events dirigido pelo setting
// `audit_retention_days` (int; 0/vazio = infinito, default). Roda logo após a
// daily automática, só no líder — 1×/dia é suficiente e não compete com o hot
// path do tick. Deleta em LOTES (auditGCBatch) pra não segurar lock/WAL quando
// há anos de eventos acumulados; loga quantas linhas saíram.
package scheduler

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Dr0nj/regente-server/internal/db"
)

// auditGCBatch — linhas por DELETE. Var (não const) pra teste exercitar o loop
// de lotes sem inserir 10k+ linhas.
var auditGCBatch = 10_000

// retentionDays lê settings.audit_retention_days. <=0 (ou inválido) = sem GC.
func (s *Scheduler) retentionDays() int {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key='audit_retention_days'`).Scan(&v); err != nil {
		return 0
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("[scheduler] settings audit_retention_days %q inválido (esperado inteiro ≥ 0) — retenção desligada", v)
		return 0
	}
	return n
}

// auditGC remove eventos mais velhos que a retenção configurada.
func (s *Scheduler) auditGC() {
	days := s.retentionDays()
	if days <= 0 {
		return // infinito (default)
	}
	for _, tbl := range []string{"instance_events", "audit_events"} {
		if n := s.auditGCTable(tbl, days); n > 0 {
			log.Printf("[scheduler] auditoria: retenção de %dd — %d linhas removidas de %s", days, n, tbl)
		}
	}
}

// auditGCTable deleta em lotes até esvaziar o excedente. O corte temporal é
// calculado pelo PRÓPRIO banco (datetime('now')/now()), não por parâmetro
// time.Time — o formato de armazenamento do DATETIME difere entre SQLite
// (texto 'YYYY-MM-DD HH:MM:SS' UTC) e Postgres (TIMESTAMPTZ), e um bind
// cruzado compararia formatos diferentes na fronteira.
func (s *Scheduler) auditGCTable(table string, days int) int {
	var q string
	var args []any
	switch s.db.Dialect() {
	case db.Postgres:
		q = `DELETE FROM ` + table + ` WHERE id IN (SELECT id FROM ` + table + ` WHERE ts < now() - make_interval(days => ?) LIMIT ?)`
		args = []any{days, auditGCBatch}
	default: // SQLite
		q = `DELETE FROM ` + table + ` WHERE id IN (SELECT id FROM ` + table + ` WHERE ts < datetime('now', ?) LIMIT ?)`
		args = []any{fmt.Sprintf("-%d days", days), auditGCBatch}
	}
	total := 0
	for {
		res, err := s.db.Exec(q, args...)
		if err != nil {
			log.Printf("[scheduler] auditoria: GC %s: %v", table, err)
			return total
		}
		n, _ := res.RowsAffected()
		total += int(n)
		if int(n) < auditGCBatch {
			return total
		}
	}
}
