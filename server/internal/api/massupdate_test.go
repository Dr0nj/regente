// Testes do CTM-3 — Mass Update rico (critério, operações e undo stack).
package api

import (
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func muDefs() []domain.JobDefinition {
	return []domain.JobDefinition{
		{ID: "fin-carga", Label: "Carga FIN", Team: "FIN", JobType: "COMMAND",
			Schedule: domain.Schedule{Enabled: true, RunAt: "06:00"}},
		{ID: "fin-envio", Label: "Envio FIN", Team: "FIN", JobType: "HTTP",
			Params:   map[string]interface{}{"url": "http://legacy-host/api", "method": "POST"},
			Schedule: domain.Schedule{Enabled: true, Description: "envia pro gateway"}},
		{ID: "data-etl", Label: "ETL", Team: "DATA", JobType: "COMMAND",
			Schedule: domain.Schedule{Enabled: false}},
	}
}

func TestSelectDefs_Criteria(t *testing.T) {
	defs := muDefs()

	// regex sobre campo
	sel, err := massCriteria{Field: "id", Regex: "^fin-"}.selectDefs(defs)
	if err != nil || len(sel) != 2 {
		t.Fatalf("regex ^fin-: %v %d", err, len(sel))
	}
	// campo vazio (description)
	sel, err = massCriteria{FieldEmpty: "description"}.selectDefs(defs)
	if err != nil || len(sel) != 2 {
		t.Fatalf("description vazia: %v %d", err, len(sel))
	}
	// jobType + folder
	sel, err = massCriteria{Folders: []string{"FIN"}, JobType: "http"}.selectDefs(defs)
	if err != nil || len(sel) != 1 || sel[0].ID != "fin-envio" {
		t.Fatalf("folder+jobType: %v %+v", err, sel)
	}
	// ids restringem
	sel, err = massCriteria{IDs: []string{"data-etl"}, Field: "id", Regex: "."}.selectDefs(defs)
	if err != nil || len(sel) != 1 || sel[0].ID != "data-etl" {
		t.Fatalf("ids: %v %+v", err, sel)
	}
	// regex inválido é erro
	if _, err = (massCriteria{Field: "id", Regex: "("}).selectDefs(defs); err == nil {
		t.Fatal("regex inválido deveria falhar")
	}
}

func TestApplyOperation_SetFieldOnlyIfEmpty(t *testing.T) {
	d := muDefs()[0] // description vazia
	ch, err := applyOperation(&d, massOperation{Op: "set-field", Field: "description", Value: "preenchida em lote", OnlyIfEmpty: true})
	if err != nil || len(ch) != 1 {
		t.Fatalf("set-field vazio: %v %+v", err, ch)
	}
	if d.Schedule.Description != "preenchida em lote" {
		t.Fatalf("description não setada: %q", d.Schedule.Description)
	}
	// Já preenchida + onlyIfEmpty → no-op
	ch, err = applyOperation(&d, massOperation{Op: "set-field", Field: "description", Value: "outra", OnlyIfEmpty: true})
	if err != nil || len(ch) != 0 {
		t.Fatalf("onlyIfEmpty deveria pular: %v %+v", err, ch)
	}
	// Campo int com valor não-inteiro → erro
	if _, err = applyOperation(&d, massOperation{Op: "set-field", Field: "retries", Value: "x"}); err == nil {
		t.Fatal("retries com string deveria falhar")
	}
	// Campo int ok
	ch, err = applyOperation(&d, massOperation{Op: "set-field", Field: "retries", Value: float64(5)})
	if err != nil || len(ch) != 1 || d.Retries != 5 {
		t.Fatalf("retries=5: %v %+v %d", err, ch, d.Retries)
	}
}

func TestApplyOperation_FindReplace(t *testing.T) {
	d := muDefs()[1]
	// label
	ch, err := applyOperation(&d, massOperation{Op: "find-replace", Field: "label", Find: "FIN$", Replace: "Financeiro"})
	if err != nil || len(ch) != 1 || d.Label != "Envio Financeiro" {
		t.Fatalf("find-replace label: %v %+v %q", err, ch, d.Label)
	}
	// params recursivo (troca host em url)
	ch, err = applyOperation(&d, massOperation{Op: "find-replace", Field: "params", Find: "legacy-host", Replace: "new-host"})
	if err != nil || len(ch) != 1 {
		t.Fatalf("find-replace params: %v %+v", err, ch)
	}
	if d.Params["url"] != "http://new-host/api" {
		t.Fatalf("url não trocada: %v", d.Params["url"])
	}
	// id é identidade — bloqueado
	if _, err = applyOperation(&d, massOperation{Op: "find-replace", Field: "id", Find: "fin", Replace: "x"}); err == nil {
		t.Fatal("find-replace em id deveria ser bloqueado")
	}
	// sem match → no-op
	ch, err = applyOperation(&d, massOperation{Op: "find-replace", Field: "label", Find: "zzz", Replace: "y"})
	if err != nil || len(ch) != 0 {
		t.Fatalf("sem match deveria ser no-op: %v %+v", err, ch)
	}
}

func TestApplyOperation_ActionsUpstreamVariables(t *testing.T) {
	d := muDefs()[0]
	act := domain.ActionRule{On: "result", Status: "NOTOK", Do: "notify", Severity: "critical"}

	// add-action + idempotência
	if ch, err := applyOperation(&d, massOperation{Op: "add-action", Action: &act}); err != nil || len(ch) != 1 {
		t.Fatalf("add-action: %v %+v", err, ch)
	}
	if ch, err := applyOperation(&d, massOperation{Op: "add-action", Action: &act}); err != nil || len(ch) != 0 {
		t.Fatalf("add-action duplicada deveria ser no-op: %v %+v", err, ch)
	}
	// remove-action por match
	if ch, err := applyOperation(&d, massOperation{Op: "remove-action", ActionMatch: &domain.ActionRule{Do: "notify"}}); err != nil || len(ch) != 1 || len(d.Actions) != 0 {
		t.Fatalf("remove-action: %v %+v actions=%d", err, ch, len(d.Actions))
	}

	// add-upstream (self é no-op; default on-success; atualizar condition)
	if ch, _ := applyOperation(&d, massOperation{Op: "add-upstream", Upstream: &domain.Upstream{From: d.ID}}); len(ch) != 0 {
		t.Fatal("upstream de si mesmo deveria ser no-op")
	}
	if ch, err := applyOperation(&d, massOperation{Op: "add-upstream", Upstream: &domain.Upstream{From: "pre"}}); err != nil || len(ch) != 1 {
		t.Fatalf("add-upstream: %v %+v", err, ch)
	}
	if d.Upstream[0].Condition != domain.CondOnSuccess {
		t.Fatalf("condition default deveria ser on-success: %v", d.Upstream[0].Condition)
	}
	if ch, err := applyOperation(&d, massOperation{Op: "add-upstream", Upstream: &domain.Upstream{From: "pre", Condition: domain.CondOnFailure}}); err != nil || len(ch) != 1 {
		t.Fatalf("update condition upstream: %v %+v", err, ch)
	}
	if ch, err := applyOperation(&d, massOperation{Op: "remove-upstream", Upstream: &domain.Upstream{From: "pre"}}); err != nil || len(ch) != 1 || len(d.Upstream) != 0 {
		t.Fatalf("remove-upstream: %v %+v", err, ch)
	}

	// set/remove-variable
	if ch, err := applyOperation(&d, massOperation{Op: "set-variable", Key: "ENV", Val: "prod"}); err != nil || len(ch) != 1 || d.Variables["ENV"] != "prod" {
		t.Fatalf("set-variable: %v %+v", err, ch)
	}
	if ch, err := applyOperation(&d, massOperation{Op: "remove-variable", Key: "ENV"}); err != nil || len(ch) != 1 {
		t.Fatalf("remove-variable: %v %+v", err, ch)
	}

	// conditions in
	if ch, err := applyOperation(&d, massOperation{Op: "add-condition-in", Key: "carga-ok"}); err != nil || len(ch) != 1 {
		t.Fatalf("add-condition-in: %v %+v", err, ch)
	}
	if ch, err := applyOperation(&d, massOperation{Op: "remove-condition-in", Key: "carga-ok"}); err != nil || len(ch) != 1 || len(d.ConditionsIn) != 0 {
		t.Fatalf("remove-condition-in: %v %+v", err, ch)
	}
}

func TestMassUndoStore_StackAndCap(t *testing.T) {
	st := &massUndoStore{stack: map[string][]massUndoEntry{}}
	for i := 0; i < massUndoCap+5; i++ {
		st.push("sid", massUndoEntry{Label: "op"})
	}
	if st.depth("sid") != massUndoCap {
		t.Fatalf("cap da pilha: %d", st.depth("sid"))
	}
	if _, ok := st.pop("sid"); !ok {
		t.Fatal("pop deveria achar entrada")
	}
	st.clear("sid")
	if st.depth("sid") != 0 {
		t.Fatal("clear deveria zerar")
	}
	if _, ok := st.pop("sid"); ok {
		t.Fatal("pop em pilha vazia deveria falhar")
	}
}

// deepCopyDef isola o snapshot de undo das mutações (maps/slices internos).
func TestDeepCopyDef_Isolation(t *testing.T) {
	orig := muDefs()[1]
	orig.Variables = map[string]string{"A": "1"}
	orig.Upstream = []domain.Upstream{{From: "x", Condition: domain.CondOnSuccess}}
	cp, err := deepCopyDef(orig)
	if err != nil {
		t.Fatalf("deep copy: %v", err)
	}
	cp.Variables["A"] = "MUTADO"
	cp.Params["url"] = "MUTADO"
	cp.Upstream[0].From = "MUTADO"
	if orig.Variables["A"] != "1" || orig.Params["url"] != "http://legacy-host/api" || orig.Upstream[0].From != "x" {
		t.Fatal("mutação na cópia vazou pro original")
	}
}
