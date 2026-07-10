package scheduler

// ADV-5 — Archives/Retention de instances. O GC arquiva ANTES de deletar
// (NDJSON por dia, todas as colunas), respeita o default infinito, pula dia
// com RUNNING e leva junto events/conditions/daily_runs do dia arquivado.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedInstance(t *testing.T, s *Scheduler, id, day, status string) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, output) VALUES(?, 'def-a', ?, ?, datetime('now'), 'saida-'||?)`,
		id, day, status, id,
	); err != nil {
		t.Fatalf("seed instance %s: %v", id, err)
	}
}

func setupArchiveTest(t *testing.T) (*Scheduler, string) {
	t.Helper()
	s := newTestScheduler(t)
	dir := t.TempDir()
	setSetting(t, s, "archive_dir", dir)
	return s, dir
}

func TestArchiveGC_DefaultInfinitoNaoMexe(t *testing.T) {
	s, dir := setupArchiveTest(t)
	seedInstance(t, s, "i-old", "2020-01-01", "OK")

	s.archiveGC() // sem instance_retention_days → no-op
	if n := countRows(t, s, "instances"); n != 1 {
		t.Fatalf("sem setting o GC não deveria remover nada; instances=%d", n)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("sem setting não deveria arquivar nada; %d arquivos", len(entries))
	}
}

func TestArchiveGC_ArquivaEDeletaDiaVencido(t *testing.T) {
	s, dir := setupArchiveTest(t)
	setSetting(t, s, "instance_retention_days", "30")

	// Dia vencido (2 instances + evento + condition + daily_run) e dia recente.
	seedInstance(t, s, "i-1", "2020-01-01", "OK")
	seedInstance(t, s, "i-2", "2020-01-01", "NOTOK")
	seedInstance(t, s, "i-3", s.TodayDate(), "OK")
	mustExec(t, s, `INSERT INTO instance_events(instance_id, kind, message) VALUES('i-1','job.finished','ok')`)
	mustExec(t, s, `INSERT INTO conditions(name, scope_date) VALUES('c1','2020-01-01')`)
	mustExec(t, s, `INSERT OR REPLACE INTO daily_runs(order_date, started_at) VALUES('2020-01-01', datetime('now'))`)

	// Lote de 1 exercita o loop de batches.
	orig := archiveGCBatch
	archiveGCBatch = 1
	defer func() { archiveGCBatch = orig }()

	s.archiveGC()

	// Só o dia recente sobra no banco.
	if n := countRows(t, s, "instances"); n != 1 {
		t.Fatalf("esperava só a instance de hoje no banco, veio %d", n)
	}
	for tbl, want := range map[string]int{"instance_events": 0, "conditions": 0, "daily_runs": 0} {
		if n := countRows(t, s, tbl); n != want {
			t.Fatalf("%s do dia arquivado deveria sair junto; sobraram %d", tbl, n)
		}
	}

	// O NDJSON tem as 2 linhas, com colunas e output intactos.
	f, err := os.Open(filepath.Join(dir, "instances-2020-01-01.ndjson"))
	if err != nil {
		t.Fatalf("archive não escrito: %v", err)
	}
	defer f.Close()
	var lines []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("linha NDJSON inválida: %v", err)
		}
		lines = append(lines, m)
	}
	if len(lines) != 2 {
		t.Fatalf("esperava 2 linhas no archive, veio %d", len(lines))
	}
	byID := map[string]map[string]any{}
	for _, m := range lines {
		byID[m["id"].(string)] = m
	}
	if byID["i-1"] == nil || byID["i-2"] == nil {
		t.Fatalf("ids errados no archive: %v", byID)
	}
	if byID["i-2"]["status"] != "NOTOK" || byID["i-1"]["output"] != "saida-i-1" {
		t.Fatalf("colunas dropadas no archive: %v", byID)
	}
	// Sem .part sobrando (escrita atômica).
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf(".part não deveria sobreviver: %s", e.Name())
		}
	}
}

func TestArchiveGC_DiaComRunningEhPulado(t *testing.T) {
	s, dir := setupArchiveTest(t)
	setSetting(t, s, "instance_retention_days", "30")
	seedInstance(t, s, "i-run", "2020-01-01", "RUNNING")
	seedInstance(t, s, "i-ok", "2020-01-01", "OK")

	s.archiveGC()
	if n := countRows(t, s, "instances"); n != 2 {
		t.Fatalf("dia com RUNNING não pode ser arquivado; instances=%d", n)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("dia com RUNNING não deveria gerar archive")
	}
}

func TestArchiveGC_NuncaSobrescreveArchiveExistente(t *testing.T) {
	s, dir := setupArchiveTest(t)
	setSetting(t, s, "instance_retention_days", "30")
	seedInstance(t, s, "i-x", "2020-01-01", "OK")
	// Simula crash pós-rename de rodada anterior: o arquivo do dia já existe.
	prev := filepath.Join(dir, "instances-2020-01-01.ndjson")
	if err := os.WriteFile(prev, []byte(`{"id":"antigo"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.archiveGC()
	if b, _ := os.ReadFile(prev); !strings.Contains(string(b), "antigo") {
		t.Fatal("archive existente foi sobrescrito — perda de dado")
	}
	if _, err := os.Stat(filepath.Join(dir, "instances-2020-01-01-2.ndjson")); err != nil {
		t.Fatalf("re-archive deveria ir pro nome sufixado: %v", err)
	}
	if n := countRows(t, s, "instances"); n != 0 {
		t.Fatalf("instances deveriam sair após o re-archive; sobraram %d", n)
	}
}

func TestArchiveGC_SettingInvalidoDesliga(t *testing.T) {
	s, _ := setupArchiveTest(t)
	seedInstance(t, s, "i-1", "2020-01-01", "OK")
	for _, v := range []string{"0", "", "abc", "-5"} {
		setSetting(t, s, "instance_retention_days", v)
		s.archiveGC()
		if n := countRows(t, s, "instances"); n != 1 {
			t.Fatalf("instance_retention_days=%q deveria desligar o GC; instances=%d", v, n)
		}
	}
}

func mustExec(t *testing.T, s *Scheduler, q string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(q, args...); err != nil {
		t.Fatalf("exec %s: %v", q, err)
	}
}
