package jobdsl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
	"gopkg.in/yaml.v3"
)

func buildSample() *Workspace {
	w := New()
	fin := w.Folder("financeiro")
	extract := fin.Job("extract-vendas", "Extração de vendas").
		Description("dump diário do ERP").
		Command("python extract.py").
		RunAt("06:00").Retries(2).RetryDelay(30).
		Weekly("mon", "tue", "wed", "thu", "fri").
		Produces("VENDAS_OK")
	fin.Job("load-dw", "Carga no DW").
		Command("python load.py").
		After(extract).Needs("VENDAS_OK").
		SLA(30, "08:00").
		NotifyOnFailure("Carga do DW falhou")
	return w
}

// REGRA: o YAML gerado é o MESMO dialeto do workspace — parseia de volta
// (estrito) para a JobDefinition equivalente. Roundtrip = compatibilidade.
func TestDSL_RoundtripsToWorkspaceYAML(t *testing.T) {
	w := buildSample()
	if errs := w.Validate(); len(errs) != 0 {
		t.Fatalf("workspace válido não podia ter erros: %v", errs)
	}

	dir := t.TempDir()
	if err := w.WriteTo(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "financeiro", "load-dw.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true) // parse estrito: o DSL não pode emitir campo que o server não conhece
	var def domain.JobDefinition
	if err := dec.Decode(&def); err != nil {
		t.Fatalf("YAML gerado não parseia estrito: %v\n%s", err, raw)
	}
	if def.ID != "load-dw" || def.Team != "financeiro" || def.JobType != "COMMAND" {
		t.Fatalf("roundtrip perdeu campos básicos: %+v", def)
	}
	if len(def.Upstream) != 1 || def.Upstream[0].From != "extract-vendas" || def.Upstream[0].Condition != domain.CondOnSuccess {
		t.Fatalf("After deveria virar upstream on-success de extract-vendas: %+v", def.Upstream)
	}
	if def.SLA == nil || def.SLA.ExpectedDurationMin != 30 || def.SLA.DeadlineHM != "08:00" {
		t.Fatalf("SLA perdido no roundtrip: %+v", def.SLA)
	}
	if len(def.Actions) != 1 || def.Actions[0].Do != "notify" {
		t.Fatalf("NotifyOnFailure deveria virar action notify: %+v", def.Actions)
	}
}

// REGRA: retryDelayMin (D-1) e selfService (D-14) fluem pelo DSL até o YAML.
func TestDSL_NewFieldsFlow(t *testing.T) {
	w := New()
	w.Folder("ops").Job("aprova-fechamento", "Aprovação do fechamento").
		Command("closing.sh").Confirm().SelfService().Retries(3).RetryDelay(3 * 24 * 60)
	out := w.YAML()
	for _, want := range []string{"retryDelayMin: 4320", "selfService: true", "confirm: true"} {
		if !strings.Contains(out, want) {
			t.Errorf("YAML deveria conter %q:\n%s", want, out)
		}
	}
}

// REGRA: Validate barra id duplicado, label vazio e dep para job inexistente —
// e WriteTo se recusa a escrever um workspace inválido.
func TestDSL_ValidateCatchesStructuralErrors(t *testing.T) {
	w := New()
	f := w.Folder("x")
	f.Job("a", "A").Command("true")
	f.Job("a", "A de novo").Command("true")           // id duplicado
	f.Job("b", "").Command("true")                    // label vazio
	f.Job("c", "C").Command("true").After("fantasma") // dep inexistente

	errs := w.Validate()
	if len(errs) != 3 {
		t.Fatalf("esperava 3 erros (dup, label, dep), veio %d: %v", len(errs), errs)
	}
	if err := w.WriteTo(t.TempDir()); err == nil {
		t.Fatal("WriteTo deveria recusar workspace inválido")
	}
}

// REGRA: output determinístico (ordenado por folder/id) — diffs de Git limpos.
func TestDSL_DeterministicOutput(t *testing.T) {
	a, b := buildSample().YAML(), buildSample().YAML()
	if a != b {
		t.Fatal("YAML() deveria ser determinístico")
	}
	if strings.Index(a, "extract-vendas") > strings.Index(a, "load-dw") {
		t.Fatal("jobs deveriam sair ordenados por id")
	}
}
