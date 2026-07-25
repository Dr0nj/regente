package scheduler

// OL — Output × Logs. Testa o armazenamento do sysout por tentativa
// (instance_output), o cap com aviso, a retenção própria e o evento `wait`
// edge-triggered.

import (
	"strings"
	"testing"
)

// readOutput concatena os chunks de uma tentativa na ordem do seq (id).
func readOutput(t *testing.T, s *Scheduler, id string, attempt int) string {
	t.Helper()
	rows, err := s.db.Query(`SELECT chunk FROM instance_output WHERE instance_id=? AND attempt=? ORDER BY id`, id, attempt)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		sb.WriteString(c)
	}
	return sb.String()
}

// OL-1 — o sysout é gravado na tentativa CORRENTE e o retry NÃO perde o da
// tentativa anterior (a promessa central da trilha).
func TestAppendOutput_PreservaTentativaNoRetry(t *testing.T) {
	s := newTestScheduler(t)
	seedInstance(t, s, "i-1", s.TodayDate(), "RUNNING")

	s.appendOutput("i-1", "line1\n")
	s.appendOutput("i-1", "line2\n")
	if got := readOutput(t, s, "i-1", 1); got != "line1\nline2\n" {
		t.Fatalf("tentativa 1 concat = %q", got)
	}

	// Simula o retry: attempts avança, orçamento reabre, nova saída isolada.
	mustExec(t, s, `UPDATE instances SET attempts=2 WHERE id='i-1'`)
	s.resetOutputBudget("i-1")
	s.appendOutput("i-1", "retry-out\n")

	if got := readOutput(t, s, "i-1", 2); got != "retry-out\n" {
		t.Fatalf("tentativa 2 concat = %q", got)
	}
	if got := readOutput(t, s, "i-1", 1); got != "line1\nline2\n" {
		t.Fatalf("tentativa 1 foi perdida no retry: %q", got)
	}
}

// OL-1 — o sysout NÃO vira mais evento kind=output (sai da trilha de auditoria).
func TestAppendOutput_NaoEmiteEvento(t *testing.T) {
	s := newTestScheduler(t)
	seedInstance(t, s, "i-1", s.TodayDate(), "RUNNING")
	s.appendOutput("i-1", "hello\n")

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id='i-1' AND kind='output'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("appendOutput não deveria gravar instance_events kind=output; veio %d", n)
	}
}

// OL-1 — cap por instance: além do teto grava UMA linha de aviso e descarta o
// resto; resetOutputBudget reabre o orçamento.
func TestAppendOutput_CapAvisaETrunca(t *testing.T) {
	s := newTestScheduler(t)
	orig := outputMaxBytes
	outputMaxBytes = 15
	defer func() { outputMaxBytes = orig }()

	seedInstance(t, s, "i-1", s.TodayDate(), "RUNNING")
	s.appendOutput("i-1", "0123456789") // 10 bytes: abaixo do teto → gravado
	s.appendOutput("i-1", "abcdefghij") // cruza 15 → vira aviso, não o chunk
	s.appendOutput("i-1", "MAIS")       // já truncado → descartado em silêncio

	got := readOutput(t, s, "i-1", 1)
	if !strings.Contains(got, "0123456789") {
		t.Fatalf("o que cabia antes do teto deveria estar presente: %q", got)
	}
	if strings.Contains(got, "abcdefghij") || strings.Contains(got, "MAIS") {
		t.Fatalf("conteúdo além do teto não deveria ser gravado: %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("faltou a linha de aviso de truncamento: %q", got)
	}

	// Reabre o orçamento (retry/finish): volta a gravar.
	s.resetOutputBudget("i-1")
	mustExec(t, s, `UPDATE instances SET attempts=2 WHERE id='i-1'`)
	s.appendOutput("i-1", "nova tentativa\n")
	if got := readOutput(t, s, "i-1", 2); got != "nova tentativa\n" {
		t.Fatalf("após reset o orçamento deveria reabrir: %q", got)
	}
}

// OL-1 — retenção própria: output_retention_days remove sysout velho; default
// (sem setting) é infinito.
func TestOutputGC_Retencao(t *testing.T) {
	s := newTestScheduler(t)
	seedInstance(t, s, "i-1", s.TodayDate(), "RUNNING")
	mustExec(t, s, `INSERT INTO instance_output(instance_id, attempt, ts, chunk) VALUES('i-1',1,datetime('now','-10 days'),'velho')`)
	mustExec(t, s, `INSERT INTO instance_output(instance_id, attempt, chunk) VALUES('i-1',1,'novo')`)

	// Sem setting → infinito, não mexe.
	s.outputGC()
	if n := countRows(t, s, "instance_output"); n != 2 {
		t.Fatalf("sem output_retention_days o GC não deveria remover nada; veio %d", n)
	}

	// 5 dias → o de 10 dias sai, o de agora fica.
	setSetting(t, s, "output_retention_days", "5")
	s.outputGC()
	if n := countRows(t, s, "instance_output"); n != 1 {
		t.Fatalf("retenção de 5d deveria deixar 1 linha; veio %d", n)
	}
	if got := readOutput(t, s, "i-1", 1); got != "novo" {
		t.Fatalf("a linha recente deveria sobreviver; veio %q", got)
	}
}

// OL-4 — o evento `wait` é EDGE-TRIGGERED: mesmo motivo repetido não re-emite;
// motivo diferente emite; startInstance limpa o dedup.
func TestMaybeEmitWait_EdgeTriggered(t *testing.T) {
	s := newTestScheduler(t)
	seedInstance(t, s, "i-1", s.TodayDate(), "WAITING")

	countWait := func() int {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id='i-1' AND kind='wait'`).Scan(&n); err != nil {
			t.Fatalf("count wait: %v", err)
		}
		return n
	}

	condA := Blocker{Kind: GateCondition, Detail: "aguardando condição A"}
	condB := Blocker{Kind: GateCondition, Detail: "aguardando condição B"}

	s.maybeEmitWait("i-1", condA)
	s.maybeEmitWait("i-1", condA) // mesmo motivo → dedup
	if n := countWait(); n != 1 {
		t.Fatalf("motivo repetido não deveria re-emitir; wait events=%d", n)
	}
	s.maybeEmitWait("i-1", condB) // motivo mudou → emite
	if n := countWait(); n != 2 {
		t.Fatalf("motivo diferente deveria emitir; wait events=%d", n)
	}

	// Saiu do WAITING e voltou a bloquear no MESMO motivo → volta a emitir.
	s.clearWaitReason("i-1")
	s.maybeEmitWait("i-1", condB)
	if n := countWait(); n != 3 {
		t.Fatalf("após clearWaitReason o mesmo motivo deveria re-emitir; wait events=%d", n)
	}
}
