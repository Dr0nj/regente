// Testes do job-as-code (parse YAML multi-doc, plano e round-trip).
package api

import (
	"strings"
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func codeScope(folders ...string) map[string]bool {
	m := map[string]bool{}
	for _, f := range folders {
		m[f] = true
	}
	return m
}

func TestParseCodeDocs_DefaultsAndErrors(t *testing.T) {
	scope := codeScope("FIN")
	code := `# comentário solto
---
# definitions/FIN/a.yaml
id: a
label: Job A
jobType: COMMAND
schedule:
  enabled: true
---
id: b
label: Job B
team: FIN
jobType: COMMAND
schedule:
  enabled: false
`
	defs, errs := parseCodeDocs(code, scope)
	if len(errs) != 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
	if len(defs) != 2 {
		t.Fatalf("esperados 2 docs, vieram %d", len(defs))
	}
	if defs[0].Team != "FIN" {
		t.Fatalf("team ausente deveria herdar a folder única do escopo, veio %q", defs[0].Team)
	}

	// Typo de campo (estrito), id duplicado e team fora do escopo → erros.
	bad := `id: a
label: A
team: FIN
jobType: COMMAND
schedule: {enabled: true}
retires: 3
---
id: a
label: dup
team: FIN
jobType: COMMAND
schedule: {enabled: true}
---
id: c
label: C
team: OUTRA
jobType: COMMAND
schedule: {enabled: true}
`
	_, errs = parseCodeDocs(bad, scope)
	if len(errs) == 0 {
		t.Fatal("esperava erros (typo estrito)")
	}
	joined := strings.Join(errs, " | ")
	if !strings.Contains(joined, "retires") {
		t.Fatalf("erro de campo desconhecido deveria citar 'retires': %v", errs)
	}
}

func TestBuildCodePlan_CreatesUpdatesDeletes(t *testing.T) {
	scope := codeScope("FIN", "DATA")
	existing := []domain.JobDefinition{
		{ID: "keep", Label: "K", Team: "FIN", JobType: "COMMAND"},
		{ID: "mod", Label: "old", Team: "FIN", JobType: "COMMAND"},
		{ID: "gone", Label: "G", Team: "DATA", JobType: "COMMAND"},
		{ID: "fora", Label: "F", Team: "RISK", JobType: "COMMAND"}, // fora do escopo: intocável
	}
	desired := []domain.JobDefinition{
		{ID: "keep", Label: "K", Team: "FIN", JobType: "COMMAND"},
		{ID: "mod", Label: "NEW", Team: "FIN", JobType: "COMMAND"},
		{ID: "novo", Label: "N", Team: "DATA", JobType: "COMMAND"},
	}
	plan, prev := buildCodePlan(existing, desired, scope)
	if len(plan.Creates) != 1 || plan.Creates[0] != "novo" {
		t.Fatalf("creates: %v", plan.Creates)
	}
	if len(plan.Updates) != 1 || plan.Updates[0] != "mod" {
		t.Fatalf("updates: %v", plan.Updates)
	}
	if len(plan.Deletes) != 1 || plan.Deletes[0] != "gone" {
		t.Fatalf("deletes: %v", plan.Deletes)
	}
	if plan.Unchanged != 1 {
		t.Fatalf("unchanged: %d", plan.Unchanged)
	}
	if _, ok := prev["fora"]; ok {
		t.Fatal("def fora do escopo não pode entrar no plano")
	}
}

// Round-trip: o YAML gerado pelo GET parseia limpo e o plano é 100% unchanged.
func TestCodeRoundTrip_NoDiff(t *testing.T) {
	scope := codeScope("FIN")
	defs := []domain.JobDefinition{
		{
			ID: "carga", Label: "Carga diária", Team: "FIN", JobType: "COMMAND",
			Schedule: domain.Schedule{Enabled: true, RunAt: "06:00", Frequency: "businessday", NthBusinessDays: []int{1, -1}},
			Retries:  2, Timeout: 300,
			Params:   map[string]interface{}{"command": "run.sh %%ODATE"},
			Upstream: []domain.Upstream{{From: "pre", Condition: domain.CondOnSuccess}},
			Actions:  []domain.ActionRule{{On: "result", Status: "NOTOK", Do: "notify", Severity: "critical"}},
		},
		{
			ID: "pre", Label: "Pré", Team: "FIN", JobType: "COMMAND",
			Schedule:  domain.Schedule{Enabled: true, Description: "prepara staging"},
			Variables: map[string]string{"ENV": "prod"},
		},
	}
	code, n := marshalDefsAsCode(defs, scope)
	if n != 2 {
		t.Fatalf("esperados 2 no documento, veio %d", n)
	}
	parsed, errs := parseCodeDocs(code, scope)
	if len(errs) != 0 {
		t.Fatalf("round-trip com erros: %v", errs)
	}
	plan, _ := buildCodePlan(defs, parsed, scope)
	if len(plan.Creates)+len(plan.Updates)+len(plan.Deletes) != 0 || plan.Unchanged != 2 {
		t.Fatalf("round-trip deveria ser 100%% unchanged: %+v", plan)
	}
}
