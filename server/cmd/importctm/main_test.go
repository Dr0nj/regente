package main

// E6 — golden test do importador: a fixture testdata/export.xml cobre cada
// mapeamento documentado no README; -dry-run não escreve nada; o relatório
// lista os não-mapeados.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
	"gopkg.in/yaml.v3"
)

func importFixture(t *testing.T, extraArgs ...string) (out string, stdout string) {
	t.Helper()
	dir := t.TempDir()
	var buf bytes.Buffer
	args := append([]string{"-in", filepath.Join("testdata", "export.xml"), "-out", dir}, extraArgs...)
	if err := run(args, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	return dir, buf.String()
}

func loadDef(t *testing.T, out, team, id string) (domain.JobDefinition, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(out, "definitions", team, id+".yaml"))
	if err != nil {
		t.Fatalf("ler %s/%s: %v", team, id, err)
	}
	var def domain.JobDefinition
	if err := yaml.Unmarshal(raw, &def); err != nil {
		t.Fatalf("YAML inválido em %s: %v", id, err)
	}
	return def, string(raw)
}

func TestImport_MapeamentosGolden(t *testing.T) {
	out, _ := importFixture(t)

	// EXTRACT_FIN — Command→COMMAND, CMDLINE, TIMEFROM→runAt, MAXRERUN→retries,
	// VARIABLE→variables; OUTCOND com 1 emissor vira ARESTA no consumidor (não
	// polui conditionsOutAdd).
	ex, _ := loadDef(t, out, "FIN", "extract_fin")
	if ex.JobType != "COMMAND" || ex.Params["command"] != "/opt/etl/extract.sh %%ODATE" {
		t.Fatalf("extract_fin: jobType/params errados: %+v", ex)
	}
	if ex.Schedule.RunAt != "01:30" || ex.Retries != 2 {
		t.Fatalf("extract_fin: runAt/retries errados: %+v", ex.Schedule)
	}
	if ex.Variables["TARGET"] != "s3://bucket/fin" {
		t.Fatalf("extract_fin: variable TARGET não importada: %+v", ex.Variables)
	}
	if len(ex.ConditionsOutAdd) != 0 {
		t.Fatalf("cond de 1 emissor virou aresta — não deveria sobrar conditionsOutAdd: %v", ex.ConditionsOutAdd)
	}

	// LOAD_FIN — INCOND de 1 emissor → upstream on-success; INCOND sem emissor
	// no export → conditionsIn; WEEKDAYS→weekly; TIMETO→windowTo; SHOUT NOTOK
	// → action notify warning.
	ld, _ := loadDef(t, out, "FIN", "load_fin")
	if len(ld.Upstream) != 1 || ld.Upstream[0].From != "extract_fin" || ld.Upstream[0].Condition != domain.CondOnSuccess {
		t.Fatalf("load_fin: upstream esperado extract_fin/on-success, veio %+v", ld.Upstream)
	}
	if len(ld.ConditionsIn) != 1 || ld.ConditionsIn[0] != "MAINFRAME-READY" {
		t.Fatalf("load_fin: cond externa deveria virar conditionsIn, veio %v", ld.ConditionsIn)
	}
	if ld.Schedule.Frequency != "weekly" || len(ld.Schedule.DaysOfWeek) != 5 || ld.Schedule.DaysOfWeek[0] != "mon" {
		t.Fatalf("load_fin: weekly mon..fri esperado, veio %+v", ld.Schedule)
	}
	if ld.Schedule.WindowTo != "22:00" {
		t.Fatalf("load_fin: TIMETO→windowTo esperado 22:00, veio %q", ld.Schedule.WindowTo)
	}
	if len(ld.Actions) != 1 || ld.Actions[0].On != "result" || ld.Actions[0].Status != "NOTOK" ||
		ld.Actions[0].Do != "notify" || ld.Actions[0].Severity != "warning" {
		t.Fatalf("load_fin: SHOUT NOTOK deveria virar notify warning, veio %+v", ld.Actions)
	}

	// REPORT_FIN — DAYS 1,15,L → monthly [1,15,-1]; DAYSCAL/CONFCAL → 1 include
	// (dedupe); SHIFT '>' → next-businessday; SHARED-COND (2 emissores) →
	// conditionsIn, e FIN-LOAD-OK (1 emissor) → upstream.
	rp, _ := loadDef(t, out, "FIN", "report_fin")
	if rp.Schedule.Frequency != "monthly" || len(rp.Schedule.DaysOfMonth) != 3 || rp.Schedule.DaysOfMonth[2] != -1 {
		t.Fatalf("report_fin: monthly [1 15 -1] esperado, veio %+v", rp.Schedule)
	}
	if len(rp.Calendars) != 1 || rp.Calendars[0].Name != "feriados_br" || rp.Calendars[0].Mode != "include" {
		t.Fatalf("report_fin: calendar include único esperado (dedupe DAYSCAL+CONFCAL), veio %+v", rp.Calendars)
	}
	if rp.Schedule.Shift != "next-businessday" {
		t.Fatalf("report_fin: SHIFT '>' → next-businessday, veio %q", rp.Schedule.Shift)
	}
	if len(rp.Upstream) != 1 || rp.Upstream[0].From != "load_fin" {
		t.Fatalf("report_fin: upstream load_fin esperado, veio %+v", rp.Upstream)
	}
	if len(rp.ConditionsIn) != 1 || rp.ConditionsIn[0] != "SHARED-COND" {
		t.Fatalf("report_fin: SHARED-COND (2 emissores) deveria ser conditionsIn, veio %v", rp.ConditionsIn)
	}

	// WATCH_FILE — FileWatcher → FILE_WATCH + path; CYCLIC/INTERVAL → 30 min.
	wf, _ := loadDef(t, out, "FIN", "watch_file")
	if wf.JobType != "FILE_WATCH" || wf.Params["path"] != "/in/dados.csv" {
		t.Fatalf("watch_file: FILE_WATCH/path esperados, veio %+v", wf)
	}
	if !wf.Schedule.Cyclic || wf.Schedule.IntervalMin != 30 {
		t.Fatalf("watch_file: cyclic 30min esperado, veio %+v", wf.Schedule)
	}

	// EMIT_A/B — cond com 2 emissores fica como conditionsOutAdd nos DOIS;
	// SIGN '-' vira conditionsOutRemove.
	ea, _ := loadDef(t, out, "FIN", "emit_a")
	eb, _ := loadDef(t, out, "FIN", "emit_b")
	if len(ea.ConditionsOutAdd) != 1 || ea.ConditionsOutAdd[0] != "SHARED-COND" {
		t.Fatalf("emit_a: SHARED-COND deveria seguir como conditionsOutAdd, veio %v", ea.ConditionsOutAdd)
	}
	if len(eb.ConditionsOutRemove) != 1 || eb.ConditionsOutRemove[0] != "OLD-COND" {
		t.Fatalf("emit_b: SIGN '-' deveria virar conditionsOutRemove, veio %v", eb.ConditionsOutRemove)
	}

	// GHOST — Dummy → COMMAND com dryRun (roda sem fazer nada).
	gh, _ := loadDef(t, out, "FIN", "ghost")
	if gh.JobType != "COMMAND" || !gh.DryRun {
		t.Fatalf("ghost: Dummy deveria virar COMMAND dryRun, veio %+v", gh)
	}

	// ODD_JOB — atributo desconhecido vira comentário TODO-import no YAML.
	_, oddRaw := loadDef(t, out, "FIN", "odd_job")
	if !strings.Contains(oddRaw, "# TODO-import:") || !strings.Contains(oddRaw, "CRITICAL") {
		t.Fatalf("odd_job: atributo CRITICAL deveria virar TODO-import, YAML:\n%s", oddRaw)
	}

	// LEGACY_TASK — TASKTYPE desconhecido é PULADO (sem arquivo).
	if _, err := os.Stat(filepath.Join(out, "definitions", "FIN", "legacy_task.yaml")); err == nil {
		t.Fatal("legacy_task (TASKTYPE Detached) deveria ser pulado, mas o YAML existe")
	}

	// Calendar stub gerado com TODO de feriados.
	calRaw, err := os.ReadFile(filepath.Join(out, "calendars", "feriados_br.yaml"))
	if err != nil {
		t.Fatalf("calendar stub: %v", err)
	}
	if !strings.Contains(string(calRaw), "TODO-import") || !strings.Contains(string(calRaw), "FERIADOS_BR") {
		t.Fatalf("calendar stub deveria apontar o calendar original, veio:\n%s", calRaw)
	}

	// Relatório: contagens + pulados com motivo + avisos de ignorados.
	rep, err := os.ReadFile(filepath.Join(out, "import-report.md"))
	if err != nil {
		t.Fatalf("import-report.md: %v", err)
	}
	report := string(rep)
	for _, want := range []string{
		"**7 ok**", "**1 partial**", "**1 skipped**",
		"TASKTYPE \"Detached\" not supported",
		"SUB_APPLICATION=\"CORE\" ignored",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("relatório sem %q:\n%s", want, report)
		}
	}
}

func TestImport_DryRunNaoEscreve(t *testing.T) {
	out, stdout := importFixture(t, "-dry-run")
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("-dry-run não pode escrever nada; achou %d entradas em %s", len(entries), out)
	}
	if !strings.Contains(stdout, "dry-run: NOTHING was written") || !strings.Contains(stdout, "skipped") {
		t.Fatalf("dry-run deveria imprimir o relatório no stdout, veio:\n%s", stdout)
	}
}

func TestImport_FolderFilter(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	err := run([]string{"-in", filepath.Join("testdata", "export.xml"), "-out", dir, "-folder-filter", "NAO-EXISTE"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "no FOLDER") {
		t.Fatalf("filter sem match deveria dar erro claro, veio %v", err)
	}
	if err := run([]string{"-in", filepath.Join("testdata", "export.xml"), "-out", dir, "-folder-filter", "FIN"}, &buf); err != nil {
		t.Fatalf("filter FIN: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "definitions", "FIN", "extract_fin.yaml")); err != nil {
		t.Fatalf("filter FIN deveria importar a folder FIN: %v", err)
	}
}
