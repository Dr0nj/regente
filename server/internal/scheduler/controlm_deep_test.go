// Aprofundamento Control-M (2026-07-03) — cyclic runtime · CONFIRM · janela
// fechada · SET de variável em runtime · cálculo de datas (%%ODATE+3B).
package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// cyclicDef — def cyclic padrão dos testes (intervalo 5min, sem janela).
func cyclicDef(id string, intervalMin, maxRuns int, windowTo string) domain.JobDefinition {
	return domain.JobDefinition{
		ID: id, JobType: "COMMAND",
		Schedule: domain.Schedule{
			Enabled: true, Cyclic: true, IntervalMin: intervalMin,
			CyclicMaxRuns: maxRuns, WindowTo: windowTo,
		},
	}
}

// Cyclic: terminar OK re-arma a MESMA instance pra WAITING com scheduled_at
// futuro (~ now+interval), cycle_runs incrementado e attempts resetado.
func TestCyclic_RearmsOnOK(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	id := seedInst(t, s, "cy-1", today, string(domain.StatusRunning), cyclicDef("cy", 5, 0, ""))

	s.FinishInstance(id, domain.StatusOK, 0, "volta 1")

	var status string
	var schedAt time.Time
	var runs, attempts int
	if err := s.db.QueryRow(
		`SELECT status, scheduled_at, COALESCE(cycle_runs,0), COALESCE(attempts,1) FROM instances WHERE id=?`, id,
	).Scan(&status, &schedAt, &runs, &attempts); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != string(domain.StatusWaiting) {
		t.Fatalf("cyclic OK deveria re-armar pra WAITING, está %s", status)
	}
	if runs != 1 {
		t.Fatalf("cycle_runs esperado 1, veio %d", runs)
	}
	if attempts != 1 {
		t.Fatalf("attempts deveria resetar pra 1 por volta, veio %d", attempts)
	}
	if d := time.Until(schedAt); d < 4*time.Minute || d > 6*time.Minute {
		t.Fatalf("scheduled_at deveria ser ~now+5min, delta=%v", d)
	}
	var evs int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id=? AND kind='cyclic'`, id).Scan(&evs)
	if evs != 1 {
		t.Fatalf("esperado 1 evento cyclic, veio %d", evs)
	}
}

// Cyclic: CyclicMaxRuns atingido → NÃO re-arma (fica OK, evento cyclic-done).
func TestCyclic_MaxRunsStops(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	id := seedInst(t, s, "cym-1", today, string(domain.StatusRunning), cyclicDef("cym", 5, 2, ""))
	if _, err := s.db.Exec(`UPDATE instances SET cycle_runs=1 WHERE id=?`, id); err != nil {
		t.Fatalf("seed cycle_runs: %v", err)
	}

	s.FinishInstance(id, domain.StatusOK, 0, "volta 2")

	if _, st, _, _ := carriedState(t, s, id); st != string(domain.StatusOK) {
		t.Fatalf("máx de voltas atingido: deveria ficar OK, está %s", st)
	}
	var done int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id=? AND kind='cyclic-done'`, id).Scan(&done)
	if done != 1 {
		t.Fatalf("esperado evento cyclic-done, veio %d", done)
	}
}

// Cyclic: próxima volta cairia DEPOIS de WindowTo → ciclo encerra.
func TestCyclic_WindowClosedStops(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	// janela até 00:01 de hoje: now+5min está garantidamente depois dela.
	id := seedInst(t, s, "cyw-1", today, string(domain.StatusRunning), cyclicDef("cyw", 5, 0, "00:01"))

	s.FinishInstance(id, domain.StatusOK, 0, "volta única")

	if _, st, _, _ := carriedState(t, s, id); st != string(domain.StatusOK) {
		t.Fatalf("janela fechada: deveria ficar OK, está %s", st)
	}
}

// Cyclic: NOTOK NÃO cicla — espera operador (rerun/Set OK), como no Control-M.
func TestCyclic_NotOKDoesNotCycle(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	id := seedInst(t, s, "cyn-1", today, string(domain.StatusRunning), cyclicDef("cyn", 5, 0, ""))

	s.FinishInstance(id, domain.StatusNotOK, 7, "falhou")

	if _, st, _, _ := carriedState(t, s, id); st != string(domain.StatusNotOK) {
		t.Fatalf("cyclic com falha deveria ficar NOTOK, está %s", st)
	}
}

// CONFIRM: def confirm:true fica no gate WAIT_CONFIRM (nem o tick nem o Force
// reivindicam) até o operador confirmar; confirmado → despacha no tick.
func TestConfirm_GateBlocksUntilConfirmed(t *testing.T) {
	s := newTestScheduler(t) // DemoMode: agente dispensado, mock-finish
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{
		ID: "cf", JobType: "COMMAND", Confirm: true,
		Schedule: domain.Schedule{Enabled: true},
	}
	id := seedInst(t, s, "cf-1", today, string(domain.StatusWaiting), def)
	forced := domain.JobDefinition{
		ID: "cff", JobType: "COMMAND", Confirm: true,
		Schedule: domain.Schedule{Enabled: true},
	}
	fid := seedInst(t, s, "cff-1", today, string(domain.StatusWaiting), forced)
	if _, err := s.db.Exec(`UPDATE instances SET forced=1 WHERE id=?`, fid); err != nil {
		t.Fatalf("mark forced: %v", err)
	}

	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _, _ := carriedState(t, s, id); st != string(domain.StatusWaiting) {
		t.Fatalf("sem Confirm o job deve permanecer WAITING, está %s", st)
	}
	if _, st, _, _ := carriedState(t, s, fid); st != string(domain.StatusWaiting) {
		t.Fatalf("forced sem Confirm também deve permanecer WAITING (Control-M), está %s", st)
	}

	// Explain expõe o gate (fonte única).
	ex, err := s.Explain(id)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if hasKind(ex.Blockers, GateConfirm) == nil {
		t.Fatalf("Explain deveria apontar WAIT_CONFIRM, veio %+v", ex.Blockers)
	}

	// Operador confirma → o tick despacha (claim síncrono no tickOnce).
	if _, err := s.db.Exec(`UPDATE instances SET confirmed=1 WHERE id IN (?,?)`, id, fid); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	s.Tick()
	if _, st, _, _ := carriedState(t, s, id); st == string(domain.StatusWaiting) {
		t.Fatal("confirmado, o job deveria ter sido reivindicado no tick")
	}
	if _, st, _, _ := carriedState(t, s, fid); st == string(domain.StatusWaiting) {
		t.Fatal("forced confirmado deveria ter sido reivindicado no tick")
	}
}

// Janela fechada (Control-M time window): WAITING depois de WindowTo não
// submete mais — gate WINDOW_CLOSED; morre na virada da daily.
func TestWindowClosed_Gate(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{
		ID: "wc", JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true, WindowTo: "00:01"},
	}
	id := seedInst(t, s, "wc-1", today, string(domain.StatusWaiting), def)

	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _, _ := carriedState(t, s, id); st != string(domain.StatusWaiting) {
		t.Fatalf("janela fechada: deveria permanecer WAITING, está %s", st)
	}
	ex, err := s.Explain(id)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if hasKind(ex.Blockers, GateWindowClosed) == nil {
		t.Fatalf("Explain deveria apontar WINDOW_CLOSED, veio %+v", ex.Blockers)
	}
}

// SET de variável em runtime (ctmvar): "%%SET NOME=VALOR" no output grava a
// global no VariableStore; outro job resolve %%NOME na interpolação.
func TestSetVarDirective_AppliesGlobals(t *testing.T) {
	s := newTestScheduler(t)
	vars, err := storage.NewVariableStore(s.db)
	if err != nil {
		t.Fatalf("variable store: %v", err)
	}
	s.AttachVariables(vars)

	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "sv", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	id := seedInst(t, s, "sv-1", today, string(domain.StatusRunning), def)

	out := "processando…\n%%SET ULTIMA_CARGA=2026-07-03\n  %%SET STATUS_CARGA=ok\nlinha sem diretiva %%SET_NAO_VALE\n"
	s.FinishInstance(id, domain.StatusOK, 0, out)

	if v, ok := vars.Get("ULTIMA_CARGA"); !ok || v.Value != "2026-07-03" {
		t.Fatalf("ULTIMA_CARGA esperado 2026-07-03, veio %+v ok=%v", v, ok)
	}
	if v, ok := vars.Get("STATUS_CARGA"); !ok || v.Value != "ok" {
		t.Fatalf("STATUS_CARGA esperado ok, veio %+v ok=%v", v, ok)
	}
	var evs int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id=? AND kind='set-var'`, id).Scan(&evs)
	if evs != 2 {
		t.Fatalf("esperados 2 eventos set-var, vieram %d", evs)
	}

	// Job seguinte lê a global via %%NOME.
	next := domain.JobDefinition{ID: "leitor", JobType: "COMMAND"}
	ctx := s.buildVarContext(next, "leitor-1")
	got := InterpolateString("carga=%%ULTIMA_CARGA st=%%STATUS_CARGA", ctx)
	if got != "carga=2026-07-03 st=ok" {
		t.Fatalf("interpolação da global setada em runtime falhou: %q", got)
	}
}

// Cálculo de datas: %%ODATE+3 (corridos), %%ODATE+3B (úteis Mon–Fri),
// calendar com feriado via ctx.BusinessDay, e token não-calculável intacto.
func TestDateCalc_Offsets(t *testing.T) {
	// 2026-07-03 = sexta-feira.
	ctx := VarContext{Runtime: map[string]string{
		"ODATE":     "20260703",
		"ORDERDATE": "2026-07-03",
	}}

	cases := []struct{ in, want string }{
		{"%%ODATE+3", "20260706"},                    // corridos: sex+3 = seg
		{"%%ODATE-1", "20260702"},                    // ontem
		{"%%ORDERDATE+1", "2026-07-04"},              // formato preservado
		{"%%ODATE+1B", "20260706"},                   // útil: sex+1B pula o fim de semana
		{"%%ODATE+3B", "20260708"},                   // sex+3B = qua
		{"%%ODATE-1B", "20260702"},                   // -1B = quinta
		{"${var.ODATE+2B}", "20260707"},              // sintaxe ${var.} também calcula
		{"x_%%ODATE+2_y", "x_20260705_y"},            // separador claro termina o número
		{"%%ODATE-bkp", "20260703-bkp"},              // sem offset (-bkp não é número): %%ODATE simples resolve
	}
	for _, c := range cases {
		if got := InterpolateString(c.in, ctx); got != c.want {
			t.Fatalf("%q: esperado %q, veio %q", c.in, c.want, got)
		}
	}

	// BusinessDay com feriado (2026-07-06, segunda): sex+1B agora = terça 07.
	holiday := time.Date(2026, 7, 6, 0, 0, 0, 0, time.Local)
	ctx.BusinessDay = func(d time.Time) bool {
		wd := d.Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			return false
		}
		return !(d.Year() == holiday.Year() && d.Month() == holiday.Month() && d.Day() == holiday.Day())
	}
	if got := InterpolateString("%%ODATE+1B", ctx); got != "20260707" {
		t.Fatalf("com feriado na segunda, sex+1B deveria ser 20260707, veio %q", got)
	}
}

// Diretiva %%SET malformada / em excesso não explode e respeita o teto.
func TestSetVarDirective_CapAndSafety(t *testing.T) {
	s := newTestScheduler(t)
	vars, err := storage.NewVariableStore(s.db)
	if err != nil {
		t.Fatalf("variable store: %v", err)
	}
	s.AttachVariables(vars)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "svc", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	id := seedInst(t, s, "svc-1", today, string(domain.StatusRunning), def)

	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString("%%SET V")
		sb.WriteString(string(rune('A' + i%26)))
		sb.WriteString(string(rune('0' + i/26)))
		sb.WriteString("=x\n")
	}
	s.FinishInstance(id, domain.StatusOK, 0, sb.String())
	if n := len(vars.List()); n > maxSetVarsPerJob {
		t.Fatalf("teto de %d diretivas estourado: %d vars gravadas", maxSetVarsPerJob, n)
	}
}
