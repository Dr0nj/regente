package domain

// CL — testes do avaliador booleano PURO da lógica de entrada (forma DNF).
// Sem I/O: o predicado sat() simula o pool + o token $TIME. Ver
// domain/conditions.go e docs/conditions-events.md.

import (
	"reflect"
	"testing"
)

// satOf — predicado que considera satisfeitos exatamente os membros dados.
func satOf(members ...string) func(string) bool {
	set := map[string]bool{}
	for _, m := range members {
		set[m] = true
	}
	return func(m string) bool { return set[m] }
}

// Retrocompat: sem lógica, ConditionsIn é um AND implícito de TODOS os nomes.
func TestEvalLogic_NilIsFlatAND(t *testing.T) {
	flat := []string{"X", "Y"}
	if EvalConditionLogic(nil, flat, satOf("X")).Satisfied {
		t.Fatal("AND implícito não podia satisfazer só com X")
	}
	if EvalConditionLogic(nil, flat, satOf("Y")).Satisfied {
		t.Fatal("AND implícito não podia satisfazer só com Y")
	}
	if !EvalConditionLogic(nil, flat, satOf("X", "Y")).Satisfied {
		t.Fatal("AND implícito devia satisfazer com X e Y")
	}
	// Sem condições e sem lógica = sempre satisfeito (job sem gate de condição).
	if !EvalConditionLogic(nil, nil, satOf()).Satisfied {
		t.Fatal("sem condições devia satisfazer")
	}
}

// (C1 AND C2) OR C3 — dispara pelo ramo AND OU só por C3.
func TestEvalLogic_AndOrThird(t *testing.T) {
	logic := &ConditionLogic{Op: CondOpOr, Groups: []CondGroup{
		{Op: CondOpAnd, Members: []string{"C1", "C2"}},
		{Op: CondOpAnd, Members: []string{"C3"}},
	}}
	cases := []struct {
		sat  []string
		want bool
	}{
		{nil, false},
		{[]string{"C1"}, false},      // ramo AND incompleto, sem C3
		{[]string{"C1", "C2"}, true}, // ramo AND fecha
		{[]string{"C3"}, true},       // o outro ramo fecha sozinho
		{[]string{"C2", "C3"}, true}, // C3 basta
	}
	for _, c := range cases {
		if got := EvalConditionLogic(logic, []string{"C1", "C2", "C3"}, satOf(c.sat...)).Satisfied; got != c.want {
			t.Fatalf("(C1∧C2)∨C3 com sat=%v: quero %v, veio %v", c.sat, c.want, got)
		}
	}
}

// (C1 OR C2) AND C3 — precisa de C3 E de pelo menos um de C1/C2.
func TestEvalLogic_OrAndThird(t *testing.T) {
	logic := &ConditionLogic{Op: CondOpAnd, Groups: []CondGroup{
		{Op: CondOpOr, Members: []string{"C1", "C2"}},
		{Op: CondOpAnd, Members: []string{"C3"}},
	}}
	cases := []struct {
		sat  []string
		want bool
	}{
		{nil, false},
		{[]string{"C1"}, false},      // falta C3
		{[]string{"C3"}, false},      // falta o grupo OR
		{[]string{"C1", "C3"}, true}, // C1 satisfaz o OR + C3
		{[]string{"C2", "C3"}, true}, // C2 satisfaz o OR + C3
	}
	for _, c := range cases {
		if got := EvalConditionLogic(logic, []string{"C1", "C2", "C3"}, satOf(c.sat...)).Satisfied; got != c.want {
			t.Fatalf("(C1∨C2)∧C3 com sat=%v: quero %v, veio %v", c.sat, c.want, got)
		}
	}
}

// $TIME é um membro como outro qualquer: "C1 OU $TIME" fecha por condição OU
// por horário (a base do fallback temporal do CL-2).
func TestEvalLogic_TimeToken(t *testing.T) {
	logic := &ConditionLogic{Op: CondOpOr, Groups: []CondGroup{
		{Op: CondOpAnd, Members: []string{"C1"}},
		{Op: CondOpAnd, Members: []string{CondTokenTime}},
	}}
	in := []string{"C1"} // $TIME NÃO entra em ConditionsIn
	if EvalConditionLogic(logic, in, satOf()).Satisfied {
		t.Fatal("sem C1 e sem horário não podia satisfazer")
	}
	if !EvalConditionLogic(logic, in, satOf("C1")).Satisfied {
		t.Fatal("C1 deveria antecipar (dispara pela condição)")
	}
	if !EvalConditionLogic(logic, in, satOf(CondTokenTime)).Satisfied {
		t.Fatal("$TIME deveria disparar pelo horário sem a condição")
	}
}

// Membros AVULSOS de ConditionsIn (fora da lógica) são requisitos AND — a
// setinha adiciona condição simples num job com lógica OR e ela é obrigatória.
func TestEvalLogic_LooseMembersAreRequired(t *testing.T) {
	logic := &ConditionLogic{Op: CondOpOr, Groups: []CondGroup{
		{Op: CondOpAnd, Members: []string{"C1"}},
		{Op: CondOpAnd, Members: []string{"C2"}},
	}}
	// LOOSE = "C3" (na entrada, fora da lógica). Precisa de (C1 OU C2) E C3.
	in := []string{"C1", "C2", "C3"}
	if EvalConditionLogic(logic, in, satOf("C1")).Satisfied {
		t.Fatal("C1 fecha o OR mas falta o avulso C3 — não podia satisfazer")
	}
	if EvalConditionLogic(logic, in, satOf("C3")).Satisfied {
		t.Fatal("C3 sozinho não fecha o grupo OR")
	}
	if !EvalConditionLogic(logic, in, satOf("C1", "C3")).Satisfied {
		t.Fatal("C1 (OR) + C3 (avulso) deveria satisfazer")
	}
	// RenderExpr mostra o avulso ANDado com parênteses no OR de topo.
	got := EvalConditionLogic(logic, in, satOf()).RenderExpr()
	if got != "(C1 OU C2) E C3" {
		t.Fatalf("RenderExpr com avulso: quero \"(C1 OU C2) E C3\", veio %q", got)
	}
}

// Operador vazio/desconhecido = AND (default seguro).
func TestEvalLogic_UnknownOpIsAnd(t *testing.T) {
	logic := &ConditionLogic{Op: "", Groups: []CondGroup{
		{Op: "xpto", Members: []string{"C1", "C2"}},
	}}
	if EvalConditionLogic(logic, []string{"C1", "C2"}, satOf("C1")).Satisfied {
		t.Fatal("grupo com op desconhecido devia ser AND (não fecha só com C1)")
	}
	if !EvalConditionLogic(logic, []string{"C1", "C2"}, satOf("C1", "C2")).Satisfied {
		t.Fatal("grupo AND devia fechar com C1 e C2")
	}
}

// RenderExpr — expressão legível pro Explain.
func TestEvalLogic_RenderExpr(t *testing.T) {
	logic := &ConditionLogic{Op: CondOpOr, Groups: []CondGroup{
		{Op: CondOpAnd, Members: []string{"C1", "C2"}},
		{Op: CondOpAnd, Members: []string{"C3"}},
	}}
	got := EvalConditionLogic(logic, nil, satOf()).RenderExpr()
	if got != "(C1 E C2) OU C3" {
		t.Fatalf("RenderExpr: quero \"(C1 E C2) OU C3\", veio %q", got)
	}
	orAnd := &ConditionLogic{Op: CondOpAnd, Groups: []CondGroup{
		{Op: CondOpOr, Members: []string{"C1", "C2"}},
		{Op: CondOpAnd, Members: []string{"C3"}},
	}}
	if got := EvalConditionLogic(orAnd, nil, satOf()).RenderExpr(); got != "(C1 OU C2) E C3" {
		t.Fatalf("RenderExpr: quero \"(C1 OU C2) E C3\", veio %q", got)
	}
}

// NormalizeConditions garante membros não-$TIME ⊆ ConditionsIn, poda grupos
// vazios e descarta lógica vazia (vira nil = AND implícito).
func TestNormalize_ConditionLogicSuperset(t *testing.T) {
	defs := NormalizeConditions([]JobDefinition{
		{
			ID: "J", JobType: "COMMAND",
			ConditionsIn: []string{"C1"}, // C2/C3 só na lógica
			ConditionLogic: &ConditionLogic{Op: CondOpOr, Groups: []CondGroup{
				{Op: CondOpAnd, Members: []string{"C1", "C2"}},
				{Op: CondOpAnd, Members: []string{"C3", CondTokenTime}},
				{Op: CondOpAnd, Members: nil}, // grupo vazio: podado
			}},
		},
	})
	d := defs[0]
	for _, want := range []string{"C1", "C2", "C3"} {
		if !containsStr(d.ConditionsIn, want) {
			t.Fatalf("ConditionsIn deveria conter %s (membro da lógica), veio %v", want, d.ConditionsIn)
		}
	}
	if containsStr(d.ConditionsIn, CondTokenTime) {
		t.Fatalf("$TIME NÃO podia entrar em ConditionsIn, veio %v", d.ConditionsIn)
	}
	if d.ConditionLogic == nil || len(d.ConditionLogic.Groups) != 2 {
		t.Fatalf("grupo vazio deveria ser podado (esperava 2 grupos), veio %+v", d.ConditionLogic)
	}

	// Lógica só com grupos vazios → nil.
	empty := NormalizeConditions([]JobDefinition{
		{ID: "K", JobType: "COMMAND", ConditionLogic: &ConditionLogic{Op: CondOpOr, Groups: []CondGroup{{Op: CondOpAnd}}}},
	})
	if empty[0].ConditionLogic != nil {
		t.Fatalf("lógica só com grupo vazio deveria virar nil, veio %+v", empty[0].ConditionLogic)
	}
}

// Idempotência: normalizar duas vezes não duplica membros em ConditionsIn.
func TestNormalize_ConditionLogicIdempotent(t *testing.T) {
	first := NormalizeConditions([]JobDefinition{
		{ID: "J", JobType: "COMMAND",
			ConditionLogic: &ConditionLogic{Op: CondOpOr, Groups: []CondGroup{
				{Op: CondOpAnd, Members: []string{"C1", "C2"}},
			}}},
	})
	second := NormalizeConditions(first)
	if !reflect.DeepEqual(first[0].ConditionsIn, second[0].ConditionsIn) {
		t.Fatalf("re-normalizar mudou ConditionsIn: %v → %v", first[0].ConditionsIn, second[0].ConditionsIn)
	}
}
