// Testes CTM-1 (%%SETLOCAL — variável com escopo local por instance) e
// CTM-2 (tokens nativos de data: EOM/BOM/EOY/BOY/NEXTBD/PREVBD/FIRSTBD/LASTBD).
package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// %%SETLOCAL grava em instances.local_vars; a interpolação da MESMA instance
// resolve, outra instance NÃO enxerga e o VariableStore global fica intocado.
func TestSetLocalVar_ScopedToInstance(t *testing.T) {
	s := newTestScheduler(t)
	vars, err := storage.NewVariableStore(s.db)
	if err != nil {
		t.Fatalf("variable store: %v", err)
	}
	s.AttachVariables(vars)

	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "sl", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	id := seedInst(t, s, "sl-1", today, string(domain.StatusRunning), def)
	other := seedInst(t, s, "outro-1", today, string(domain.StatusRunning),
		domain.JobDefinition{ID: "outro", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}})

	out := "passo 1 ok\n%%SETLOCAL CURSOR=1500\n  %%SETLOCAL FASE=carga\n"
	s.FinishInstance(id, domain.StatusOK, 0, out)

	// A própria instance resolve %%CURSOR / ${var.FASE}.
	ctx := s.buildVarContext(def, id)
	if got := InterpolateString("cursor=%%CURSOR fase=${var.FASE}", ctx); got != "cursor=1500 fase=carga" {
		t.Fatalf("interpolação local falhou: %q", got)
	}
	// Outra instance NÃO enxerga (token fica intacto = visível que não resolveu).
	ctxOther := s.buildVarContext(domain.JobDefinition{ID: "outro", JobType: "COMMAND"}, other)
	if got := InterpolateString("cursor=%%CURSOR", ctxOther); got != "cursor=%%CURSOR" {
		t.Fatalf("var local vazou para outra instance: %q", got)
	}
	// Global intocado.
	if _, ok := vars.Get("CURSOR"); ok {
		t.Fatal("%%SETLOCAL não pode gravar no VariableStore global")
	}
	// Evento de auditoria dedicado.
	var evs int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id=? AND kind='set-var-local'`, id).Scan(&evs)
	if evs != 2 {
		t.Fatalf("esperados 2 eventos set-var-local, vieram %d", evs)
	}
}

// %%SETLOCAL persiste ANTES do retry: a tentativa falha passa estado pra
// próxima; e sobrevive a término NOTOK (terminal).
func TestSetLocalVar_SurvivesNotOKAndMerges(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "slr", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	id := seedInst(t, s, "slr-1", today, string(domain.StatusRunning), def)

	// 1º término (NOTOK, sem retries no def → terminal): seta CURSOR.
	s.FinishInstance(id, domain.StatusNotOK, 1, "%%SETLOCAL CURSOR=100\n")
	ctx := s.buildVarContext(def, id)
	if got := InterpolateString("%%CURSOR", ctx); got != "100" {
		t.Fatalf("var local não persistiu no NOTOK: %q", got)
	}

	// Rerun-like: novo término atualiza a MESMA var e adiciona outra (merge).
	// Um rerun de verdade volta a instance pra WAITING→RUNNING antes de
	// reexecutar; rearmamos o status aqui porque o guard idempotente do
	// FinishInstance ignora um término repetido numa instance já terminal.
	if _, err := s.db.Exec(`UPDATE instances SET status=? WHERE id=?`, string(domain.StatusRunning), id); err != nil {
		t.Fatal(err)
	}
	s.FinishInstance(id, domain.StatusOK, 0, "%%SETLOCAL CURSOR=200\n%%SETLOCAL DONE=yes\n")
	ctx = s.buildVarContext(def, id)
	if got := InterpolateString("%%CURSOR-%%DONE", ctx); got != "200-yes" {
		t.Fatalf("merge de vars locais falhou: %q", got)
	}
}

// %%SETLOCAL malformado/em massa respeita o teto e não colide com %%SET global.
func TestSetLocalVar_CapAndNoGlobalCollision(t *testing.T) {
	s := newTestScheduler(t)
	vars, err := storage.NewVariableStore(s.db)
	if err != nil {
		t.Fatalf("variable store: %v", err)
	}
	s.AttachVariables(vars)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "slc", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	id := seedInst(t, s, "slc-1", today, string(domain.StatusRunning), def)

	var sb strings.Builder
	sb.WriteString("%%SET GLOBALZAO=1\n") // global segue funcionando lado a lado
	for i := 0; i < 30; i++ {
		sb.WriteString("%%SETLOCAL L")
		sb.WriteString(string(rune('A' + i%26)))
		sb.WriteString(string(rune('0' + i/26)))
		sb.WriteString("=x\n")
	}
	s.FinishInstance(id, domain.StatusOK, 0, sb.String())

	if v, ok := vars.Get("GLOBALZAO"); !ok || v.Value != "1" {
		t.Fatalf("%%SET global deveria continuar funcionando: %+v ok=%v", v, ok)
	}
	ctx := s.buildVarContext(def, id)
	if len(ctx.Local) > maxSetVarsPerJob {
		t.Fatalf("teto de %d %%SETLOCAL estourado: %d gravadas", maxSetVarsPerJob, len(ctx.Local))
	}
}

// CTM-2 — tokens nativos como VALOR direto, derivados do ODATE.
func TestNativeDateTokens_Values(t *testing.T) {
	// 2026-07-03 = sexta-feira; julho/2026: 31 dias, termina numa sexta.
	ctx := VarContext{Runtime: map[string]string{"ODATE": "20260703"}}
	cases := []struct{ in, want string }{
		{"%%EOM", "20260731"},              // último dia do mês
		{"%%BOM", "20260701"},              // primeiro dia do mês
		{"%%EOY", "20261231"},              // último dia do ano
		{"%%BOY", "20260101"},              // primeiro dia do ano
		{"%%NEXTBD", "20260706"},           // sex → próxima segunda
		{"%%PREVBD", "20260702"},           // quinta anterior
		{"%%FIRSTBD", "20260701"},          // 1º/jul = quarta (útil)
		{"%%LASTBD", "20260731"},           // 31/jul = sexta (útil)
		{"${var.EOM}", "20260731"},         // sintaxe ${var.} também resolve
		{"%%EOM-1", "20260730"},            // offset sobre token nativo
		{"%%LASTBD-1B", "20260730"},        // penúltimo dia útil do mês
		{"%%NEXTBD+2B", "20260708"},        // compõe: próxima segunda +2 úteis = quarta
		{"%%INEXISTENTE", "%%INEXISTENTE"}, // não-nativo/não-definido fica intacto
	}
	for _, c := range cases {
		if got := InterpolateString(c.in, ctx); got != c.want {
			t.Fatalf("%q: esperado %q, veio %q", c.in, c.want, got)
		}
	}
}

// Dia útil dos tokens nativos respeita o calendar (ctx.BusinessDay) e um
// nome definido pelo usuário tem PRECEDÊNCIA sobre o nativo.
func TestNativeDateTokens_CalendarAndPrecedence(t *testing.T) {
	ctx := VarContext{Runtime: map[string]string{"ODATE": "20260703"}} // sexta
	// Feriado na segunda 2026-07-06: NEXTBD pula pra terça 07.
	ctx.BusinessDay = func(d time.Time) bool {
		wd := d.Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			return false
		}
		return !(d.Year() == 2026 && d.Month() == time.July && d.Day() == 6)
	}
	if got := InterpolateString("%%NEXTBD", ctx); got != "20260707" {
		t.Fatalf("NEXTBD com feriado na segunda deveria ser 20260707, veio %q", got)
	}
	// LASTBD com feriado no último dia útil: 31/jul (sexta) feriado → 30/jul.
	ctx.BusinessDay = func(d time.Time) bool {
		wd := d.Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			return false
		}
		return !(d.Year() == 2026 && d.Month() == time.July && d.Day() == 31)
	}
	if got := InterpolateString("%%LASTBD", ctx); got != "20260730" {
		t.Fatalf("LASTBD com feriado na sexta deveria ser 20260730, veio %q", got)
	}
	// Precedência: definition define EOM → vence o nativo.
	ctx.Definition = map[string]string{"EOM": "custom"}
	if got := InterpolateString("%%EOM", ctx); got != "custom" {
		t.Fatalf("EOM definido pelo usuário deveria vencer o nativo, veio %q", got)
	}
}
