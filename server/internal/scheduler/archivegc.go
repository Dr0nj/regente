// ADV-5 — Archives / Retention (relaciona E2 — auditgc.go, o irmão deste GC).
//
// GC diário de INSTANCES dirigido por `settings.instance_retention_days`
// (int; 0/vazio = infinito, default): dailies com order_date além da retenção
// são ARQUIVADAS antes de sair do state store — cada dia vira um NDJSON
// `instances-<order_date>.ndjson` em `settings.archive_dir` (default
// ./archive), uma linha por instance com TODAS as colunas (status, output,
// definition_snapshot, timestamps…), e só então as linhas são deletadas em
// lotes, junto com os instance_events, conditions e daily_runs do dia. Roda
// logo após a daily automática, só no líder, síncrono — mesmo racional do
// auditGC (lotes curtos, sem goroutine solta escrevendo pós-teardown).
//
// Escrita ATÔMICA (.part+rename, padrão do MFT): um crash entre o arquivo e o
// DELETE só re-arquiva o dia na próxima rodada — nunca perde dado. Os
// instance_events do dia arquivado saem juntos (senão virariam órfãos eternos
// quando audit_retention_days=infinito); o conteúdo operacional relevante já
// está na linha arquivada da instance. Dia com instance RUNNING é pulado por
// segurança (carry-over já move as vivas pra frente; isto é cinto+suspensório).
package scheduler

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// archiveGCBatch — linhas por DELETE (var pra teste exercitar o loop de lotes).
var archiveGCBatch = 10_000

// archiveGCMaxDays — dias arquivados por rodada (1×/dia): limita o custo de um
// catch-up gigante (ex.: ligar retenção com anos de histórico) sem nunca
// travar — as rodadas seguintes continuam de onde parou.
var archiveGCMaxDays = 30

// instanceRetentionDays lê settings.instance_retention_days. <=0/inválido = sem GC.
func (s *Scheduler) instanceRetentionDays() int {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key='instance_retention_days'`).Scan(&v); err != nil {
		return 0
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("[scheduler] settings instance_retention_days %q invalid (expected integer ≥ 0) — retention disabled", v)
		return 0
	}
	return n
}

// archiveDir resolve settings.archive_dir (default ./archive). A API de
// listagem/download (GET /api/archive) resolve a MESMA chave com o mesmo default.
func (s *Scheduler) archiveDir() string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key='archive_dir'`).Scan(&v)
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return "archive"
}

// archiveGC arquiva e remove as dailies além da retenção configurada.
func (s *Scheduler) archiveGC() {
	days := s.instanceRetentionDays()
	if days <= 0 {
		return // infinito (default)
	}
	// Corte pela data de NEGÓCIO (E1 — timezone da daily; DAY-1 — vira no
	// daily_at, não à meia-noite), não UTC: order_date é TEXT ISO, comparação
	// lexicográfica funciona nos dois dialetos. A subtração é sobre o LABEL do
	// dia corrente — retenção se conta em diárias, não em 24h de relógio.
	cutoff := AddDays(s.TodayDate(), -days)

	rows, err := s.db.Query(`SELECT DISTINCT order_date FROM instances WHERE order_date < ? ORDER BY order_date LIMIT ?`, cutoff, archiveGCMaxDays)
	if err != nil {
		log.Printf("[scheduler] archive: listing days: %v", err)
		return
	}
	var daysToGo []string
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil {
			daysToGo = append(daysToGo, d)
		}
	}
	// Parcial é inofensivo aqui (archiveDay é idempotente e a próxima rodada
	// pega os dias que faltaram) — só registra o porquê da rodada curta.
	if err := rows.Err(); err != nil {
		log.Printf("[scheduler] archive: incomplete day listing: %v", err)
	}
	rows.Close()

	for _, day := range daysToGo {
		if err := s.archiveDay(day); err != nil {
			log.Printf("[scheduler] archive: day %s: %v", day, err)
			return // para a rodada — a próxima re-tenta do mesmo ponto
		}
	}
}

// archiveDay escreve o NDJSON do dia (atômico) e remove as linhas do state store.
func (s *Scheduler) archiveDay(day string) error {
	// Segurança: nada RUNNING pode ser apagado do estado vivo.
	var running int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instances WHERE order_date = ? AND status = 'RUNNING'`, day).Scan(&running)
	if running > 0 {
		return fmt.Errorf("%d instance(s) RUNNING — day skipped", running)
	}

	dir := s.archiveDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	target := filepath.Join(dir, "instances-"+day+".ndjson")
	// Alvo já existe (crash pós-rename de rodada anterior, ou colisão exótica):
	// nunca sobrescrever um archive — sufixa.
	for i := 2; ; i++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		target = filepath.Join(dir, fmt.Sprintf("instances-%s-%d.ndjson", day, i))
	}

	n, err := s.writeDayNDJSON(day, target)
	if err != nil {
		return err
	}

	// Arquivo no chão (renomeado) — agora pode deletar, em lotes.
	//  1. events, output (OL-1) e execuções (ST-1) das instances do dia — senão
	//     viram órfãos eternos (mesmo tratamento: saem com a instance, sem ir
	//     para o NDJSON, que arquiva a linha da instance);
	//  2. as instances;
	//  3. conditions e daily_run do dia.
	s.batchDelete(`DELETE FROM instance_events WHERE id IN (SELECT id FROM instance_events WHERE instance_id IN (SELECT id FROM instances WHERE order_date = ?) LIMIT ?)`, day)
	s.batchDelete(`DELETE FROM instance_output WHERE id IN (SELECT id FROM instance_output WHERE instance_id IN (SELECT id FROM instances WHERE order_date = ?) LIMIT ?)`, day)
	s.batchDelete(`DELETE FROM instance_runs WHERE id IN (SELECT id FROM instance_runs WHERE instance_id IN (SELECT id FROM instances WHERE order_date = ?) LIMIT ?)`, day)
	s.batchDelete(`DELETE FROM instances WHERE id IN (SELECT id FROM instances WHERE order_date = ? LIMIT ?)`, day)
	if _, err := s.db.Exec(`DELETE FROM conditions WHERE scope_date = ?`, day); err != nil {
		log.Printf("[scheduler] archive: conditions de %s: %v", day, err)
	}
	if _, err := s.db.Exec(`DELETE FROM daily_runs WHERE order_date = ?`, day); err != nil {
		log.Printf("[scheduler] archive: daily_runs de %s: %v", day, err)
	}
	log.Printf("[scheduler] archive: %d instance(s) de %s arquivadas em %s", n, day, target)
	return nil
}

// writeDayNDJSON serializa TODAS as colunas de todas as instances do dia
// (colunas dinâmicas — o schema evolui por migration e o archive nunca pode
// dropar campo novo) num arquivo .part e o renomeia no fim. Retorna o nº de linhas.
func (s *Scheduler) writeDayNDJSON(day, target string) (int, error) {
	rows, err := s.db.Query(`SELECT * FROM instances WHERE order_date = ?`, day)
	if err != nil {
		return 0, fmt.Errorf("select: %w", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("columns: %w", err)
	}

	part := target + ".part"
	f, err := os.Create(part)
	if err != nil {
		return 0, fmt.Errorf("create: %w", err)
	}
	defer os.Remove(part) // no-op após o rename

	enc := json.NewEncoder(f)
	n := 0
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			f.Close()
			return 0, fmt.Errorf("scan: %w", err)
		}
		line := make(map[string]any, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				line[c] = string(b) // TEXT do SQLite chega como []byte
			} else {
				line[c] = vals[i]
			}
		}
		if err := enc.Encode(line); err != nil {
			f.Close()
			return 0, fmt.Errorf("encode: %w", err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	return n, os.Rename(part, target)
}

// batchDelete roda o DELETE em lotes até esvaziar (mesmo padrão do auditGC).
func (s *Scheduler) batchDelete(q, day string) int {
	total := 0
	for {
		res, err := s.db.Exec(q, day, archiveGCBatch)
		if err != nil {
			log.Printf("[scheduler] archive: delete: %v", err)
			return total
		}
		n, _ := res.RowsAffected()
		total += int(n)
		if int(n) < archiveGCBatch {
			return total
		}
	}
}
