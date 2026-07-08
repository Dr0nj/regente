package jobdsl_test

import (
	"fmt"
	"strings"

	"github.com/Dr0nj/regente-server/pkg/jobdsl"
)

// O fluxo schedule-as-code completo: definir em Go, gerar o YAML do workspace,
// validar com `regente test`, commitar no repo GitOps (a UI reflete via
// webhook/poll — o modo CODE do Design mostra exatamente este documento).
func Example() {
	w := jobdsl.New()
	fin := w.Folder("financeiro")

	extract := fin.Job("extract-vendas", "Extração de vendas").
		Description("dump diário do ERP").
		Command("python extract.py").
		RunAt("06:00").
		Weekly("mon", "tue", "wed", "thu", "fri").
		Retries(2).RetryDelay(30)

	fin.Job("load-dw", "Carga no DW").
		Command("python load.py").
		After(extract).
		SLA(30, "08:00").
		NotifyOnFailure("Carga do DW falhou")

	fmt.Println(strings.Count(w.YAML(), "---"), "jobs gerados")
	// Output: 2 jobs gerados
}
