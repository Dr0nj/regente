// OL — Output × Logs (2026-07-22): o sysout da EXECUÇÃO é separado do jornal de
// AGENDAMENTO (instance_events), com paridade Control-M.
//
//   - Output (esta trilha): o que o processo escreveu (stdout/stderr), por
//     TENTATIVA, ao vivo enquanto roda. Vive em `instance_output` (schemaV22).
//   - Log (instance_events): o que o SCHEDULER fez com o job — ordered, wait,
//     submitted, started, retry, finished, ações de operador/On-Do.
//
// Antes, cada chunk de stdout virava um evento `kind=output` na MESMA tabela da
// auditoria — inflando o feed cross-instance e o export SIEM, e perdendo o
// sysout de tentativas anteriores no retry. Aqui o chunk APPENDa em
// instance_output; `FinishInstance` segue consolidando o texto final em
// `instances.output` (compat).
package scheduler

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Dr0nj/regente-server/internal/db"
)

// outputMaxBytes — cap de sysout por instance (var, não const, p/ os testes
// exercitarem o truncamento sem gerar megabytes). Sysout tagarelo é descartado
// além disto, com UMA linha de aviso. Control-M também limita sysout.
var outputMaxBytes = 5 << 20 // 5 MiB

// outputGCBatch — linhas por DELETE na retenção (mesmo padrão do auditGC).
var outputGCBatch = 10_000

// AppendOutput — wrapper público: a API (WS/HTTP do agente) e o executor SSH
// registram um chunk de sysout da execução. Ver appendOutput.
func (s *Scheduler) AppendOutput(instanceID, chunk string) { s.appendOutput(instanceID, chunk) }

// appendOutput grava um chunk de stdout/stderr em instance_output, associado à
// TENTATIVA corrente da instance (COALESCE(attempts,1) lido na própria INSERT ...
// SELECT — sem round-trip extra). Aplica o cap por instance (outputMaxBytes):
// ao cruzá-lo, grava uma única linha de aviso e passa a descartar. Best-effort:
// falha loga e segue (igual ao emitEvent clássico); nunca bloqueia o hot path.
func (s *Scheduler) appendOutput(instanceID, chunk string) {
	if instanceID == "" || chunk == "" {
		return
	}
	s.outMu.Lock()
	if s.outTrunc[instanceID] {
		// Já cruzou o teto e já avisou — descarta em silêncio.
		s.outMu.Unlock()
		return
	}
	after := s.outBytes[instanceID] + int64(len(chunk))
	s.outBytes[instanceID] = after
	crossed := after > int64(outputMaxBytes)
	if crossed {
		s.outTrunc[instanceID] = true
	}
	s.outMu.Unlock()

	if crossed {
		// O chunk que cruzou é substituído por UMA linha de aviso (não trunca no
		// meio de uma linha); os próximos são descartados até resetOutputBudget.
		s.insertOutput(instanceID, fmt.Sprintf(
			"\n[output truncado — cap de %d bytes por instance atingido; o restante não é gravado]\n", outputMaxBytes))
		return
	}
	s.insertOutput(instanceID, chunk)
}

// insertOutput faz o INSERT ... SELECT que resolve a tentativa corrente da
// instance dentro da própria query (portável SQLite/Postgres). Se a instance
// não existir (corrida com delete), nenhuma linha casa e nada é gravado.
func (s *Scheduler) insertOutput(instanceID, chunk string) {
	_, err := s.db.Exec(
		`INSERT INTO instance_output(instance_id, attempt, chunk)
		 SELECT ?, COALESCE(attempts,1), ? FROM instances WHERE id=?`,
		instanceID, chunk, instanceID,
	)
	if err != nil {
		log.Printf("[scheduler] appendOutput %s: %v", instanceID, err)
	}
}

// resetOutputBudget re-abre o orçamento de sysout de uma instance. Chamado no
// término terminal (FinishInstance) e no re-dispatch do retry (maybeRetry) — a
// tentativa nova estreia com o teto cheio.
func (s *Scheduler) resetOutputBudget(instanceID string) {
	s.outMu.Lock()
	delete(s.outBytes, instanceID)
	delete(s.outTrunc, instanceID)
	s.outMu.Unlock()
}

// maybeEmitWait — OL-4: registra na timeline (kind=`wait`) o motivo de uma
// instance WAITING não ter rodado NESTE tick, EDGE-TRIGGERED: emite só quando o
// motivo MUDA em relação ao último gravado (dedup por instance). O tick roda a
// cada 2s; sem dedup, um job preso em WAIT COND inundaria o log. Assim o "por
// que demorou" vira HISTÓRIA consultável (ordered → wait(cond) → wait(recurso)
// → submitted → started …), não só o estado presente do Explain.
func (s *Scheduler) maybeEmitWait(instanceID string, b Blocker) {
	reason := string(b.Kind) + "|" + b.Detail
	s.mu.Lock()
	if s.waitReason[instanceID] == reason {
		s.mu.Unlock()
		return
	}
	s.waitReason[instanceID] = reason
	s.mu.Unlock()
	s.emitEvent(instanceID, "wait", "scheduler", b.Detail)
}

// clearWaitReason esquece o último motivo de espera (a instance saiu do WAITING:
// despachada, cancelada ou re-ordenada) — o próximo bloqueio volta a emitir.
func (s *Scheduler) clearWaitReason(instanceID string) {
	s.mu.Lock()
	delete(s.waitReason, instanceID)
	s.mu.Unlock()
}

// outputRetentionDays lê settings.output_retention_days. <=0 (ou inválido/vazio)
// = sem GC (infinito). Separado de audit_retention_days de propósito: sysout e
// auditoria têm valores de retenção diferentes.
func (s *Scheduler) outputRetentionDays() int {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key='output_retention_days'`).Scan(&v); err != nil {
		return 0
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("[scheduler] settings output_retention_days %q inválido (esperado inteiro ≥ 0) — retenção de output desligada", v)
		return 0
	}
	return n
}

// outputGC remove sysout mais velho que a retenção configurada, em lotes (mesmo
// padrão do auditGC). Roda 1×/dia após a daily, só no líder.
func (s *Scheduler) outputGC() {
	days := s.outputRetentionDays()
	if days <= 0 {
		return // infinito (default)
	}
	var q string
	var args []any
	switch s.db.Dialect() {
	case db.Postgres:
		q = `DELETE FROM instance_output WHERE id IN (SELECT id FROM instance_output WHERE ts < now() - make_interval(days => ?) LIMIT ?)`
		args = []any{days, outputGCBatch}
	default: // SQLite
		q = `DELETE FROM instance_output WHERE id IN (SELECT id FROM instance_output WHERE ts < datetime('now', ?) LIMIT ?)`
		args = []any{fmt.Sprintf("-%d days", days), outputGCBatch}
	}
	total := 0
	for {
		res, err := s.db.Exec(q, args...)
		if err != nil {
			log.Printf("[scheduler] output GC: %v", err)
			break
		}
		n, _ := res.RowsAffected()
		total += int(n)
		if int(n) < outputGCBatch {
			break
		}
	}
	if total > 0 {
		log.Printf("[scheduler] output: retenção de %dd — %d linhas removidas de instance_output", days, total)
	}
}
