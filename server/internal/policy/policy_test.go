package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func writePolicy(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// REGRA: sem policies.yaml não há governança — nil, sem erro, nada bloqueia.
func TestLoad_AbsentIsNoPolicy(t *testing.T) {
	p, err := Load(t.TempDir())
	if err != nil || p != nil {
		t.Fatalf("sem arquivo deveria ser (nil,nil), veio (%v,%v)", p, err)
	}
	if p.Active() || p.Blocking() {
		t.Fatal("policy nil não pode estar ativa")
	}
}

// REGRA: política QUEBRADA é erro, não política-que-não-vale (falhar aberto
// seria burlável): YAML inválido, enforcement desconhecido e regex ruim.
func TestLoad_BrokenPolicyIsError(t *testing.T) {
	for name, content := range map[string]string{
		"yaml inválido":           "enforcement: [",
		"enforcement desconhecido": "enforcement: maybe",
		"typo em regra (estrito)":  "enforcement: error\nrules:\n  requireSLAA: true",
		"regex ruim":              "enforcement: error\nrules:\n  idPattern: '['",
	} {
		dir := t.TempDir()
		writePolicy(t, dir, content)
		if _, err := Load(dir); err == nil {
			t.Errorf("%s: deveria dar erro", name)
		}
	}
}

func TestValidate_Rules(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, `
enforcement: error
exemptFolders: [sandbox]
rules:
  requireSLA: true
  requireRetries: 1
  requireDescription: true
  idPattern: "^[a-z][a-z0-9_-]*$"
  allowedJobTypes: [COMMAND, HTTP]
  maxRetries: 5
  forbidDryRun: true
`)
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Active() || !p.Blocking() {
		t.Fatal("enforcement=error deveria ser ativo e bloqueante")
	}

	good := domain.JobDefinition{
		ID: "etl-vendas", Team: "PIX", JobType: "COMMAND", Retries: 2,
		Schedule: domain.Schedule{Description: "carga diária de vendas"},
		SLA:      &domain.SLASpec{ExpectedDurationMin: 10},
	}
	if vs := p.Validate([]domain.JobDefinition{good}); len(vs) != 0 {
		t.Fatalf("def conforme não podia violar, veio %+v", vs)
	}

	bad := domain.JobDefinition{
		ID: "ETL Vendas!", Team: "PIX", JobType: "FTP", Retries: 9, DryRun: true,
	}
	vs := p.Validate([]domain.JobDefinition{bad})
	wantRules := map[string]bool{
		"requireSLA": true, "requireDescription": true, "idPattern": true,
		"allowedJobTypes": true, "maxRetries": true, "forbidDryRun": true,
	}
	got := map[string]bool{}
	for _, v := range vs {
		got[v.Rule] = true
	}
	for rule := range wantRules {
		if !got[rule] {
			t.Errorf("esperava violação %s, não veio (violações: %+v)", rule, vs)
		}
	}
	// retries=9 > max 5 mas TAMBÉM >= mínimo 1: requireRetries NÃO dispara
	if got["requireRetries"] {
		t.Error("requireRetries não devia disparar com retries=9")
	}

	// folder isenta pula tudo
	bad.Team = "sandbox"
	if vs := p.Validate([]domain.JobDefinition{bad}); len(vs) != 0 {
		t.Fatalf("folder isenta não podia violar, veio %+v", vs)
	}
}

// REGRA: enforcement=warn valida mas não bloqueia; off não valida nada.
func TestEnforcementModes(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, "enforcement: warn\nrules:\n  requireSLA: true")
	p, _ := Load(dir)
	if !p.Active() || p.Blocking() {
		t.Fatal("warn: ativo sim, bloqueante não")
	}
	if vs := p.Validate([]domain.JobDefinition{{ID: "x", Team: "t"}}); len(vs) != 1 {
		t.Fatalf("warn ainda VALIDA, veio %+v", vs)
	}

	dir2 := t.TempDir()
	writePolicy(t, dir2, "enforcement: off\nrules:\n  requireSLA: true")
	p2, _ := Load(dir2)
	if p2.Active() || p2.Validate([]domain.JobDefinition{{ID: "x"}}) != nil {
		t.Fatal("off não valida nada")
	}
}
