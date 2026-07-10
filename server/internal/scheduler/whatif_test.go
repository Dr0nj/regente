package scheduler

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// ADV-3 What-If — cadeia A → B → C (on-success) + R (on-failure de B).

func wiDefs() []domain.JobDefinition {
	mk := func(id, runAt string, ups ...domain.Upstream) domain.JobDefinition {
		return domain.JobDefinition{
			ID: id, Label: id, Team: "FIN", JobType: "COMMAND",
			Schedule: domain.Schedule{Enabled: true, RunAt: runAt},
			Upstream: ups,
		}
	}
	return []domain.JobDefinition{
		mk("a", "06:00"),
		mk("b", "", domain.Upstream{From: "a", Condition: domain.CondOnSuccess}),
		mk("c", "", domain.Upstream{From: "b", Condition: domain.CondOnSuccess}),
		mk("r", "", domain.Upstream{From: "b", Condition: domain.CondOnFailure}), // recovery
	}
}

func rowByID(t *testing.T, rep WhatIfReport, id string) WhatIfRow {
	t.Helper()
	for _, r := range rep.Rows {
		if r.DefID == id {
			return r
		}
	}
	t.Fatalf("row %s não veio no report: %+v", id, rep.Rows)
	return WhatIfRow{}
}

// Atraso propaga rio abaixo; quem está fora da cadeia não move.
func TestWhatIf_DelayPropagates(t *testing.T) {
	durs := map[string]int64{"a": 10 * 60_000, "b": 10 * 60_000, "c": 10 * 60_000}
	rep := WhatIf(wiDefs(), nil, "2026-07-10", durs,
		[]WhatIfChange{{DefID: "a", DelayMin: 30}})

	a, b, c := rowByID(t, rep, "a"), rowByID(t, rep, "b"), rowByID(t, rep, "c")
	want := int64(30 * 60_000)
	for _, r := range []WhatIfRow{a, b, c} {
		if r.State != "delayed" || r.DeltaMs != want {
			t.Fatalf("%s deveria atrasar 30min, veio state=%s delta=%d", r.DefID, r.State, r.DeltaMs)
		}
	}
	if !a.ChangeInjected || b.ChangeInjected {
		t.Fatal("changeInjected deveria marcar só o job mutado")
	}
	// baseline: a começa no runAt 06:00 e dura o p50 real
	if a.BaseStart == nil || a.BaseStart.Hour() != 6 || a.BaseEnd.Sub(*a.BaseStart) != 10*time.Minute {
		t.Fatalf("baseline de a errado: %+v", a)
	}
	// recovery não roda em nenhum dos dois mundos (b OK nos dois)
	r := rowByID(t, rep, "r")
	if r.BaseRuns || r.ScenRuns || r.State != "not-run" || r.Impacted {
		t.Fatalf("recovery não podia rodar: %+v", r)
	}
	if rep.Summary.Impacted != 3 || rep.Summary.MakespanScenMs-rep.Summary.MakespanBaseMs != want {
		t.Fatalf("summary errado: %+v", rep.Summary)
	}
}

// Falha simulada: on-success rio abaixo BLOQUEIA; recovery on-failure DESTRAVA.
func TestWhatIf_FailBlocksAndUnlocksRecovery(t *testing.T) {
	rep := WhatIf(wiDefs(), nil, "2026-07-10", nil,
		[]WhatIfChange{{DefID: "b", Fail: true}})

	if r := rowByID(t, rep, "b"); r.State != "fails" || r.ScenStatus != "NOTOK" {
		t.Fatalf("b deveria falhar no cenário: %+v", r)
	}
	if r := rowByID(t, rep, "c"); r.State != "blocked" || r.ScenRuns {
		t.Fatalf("c deveria bloquear (on-success de b): %+v", r)
	}
	rec := rowByID(t, rep, "r")
	if rec.State != "starts-running" || !rec.ScenRuns || rec.BaseRuns {
		t.Fatalf("recovery deveria passar a rodar no cenário: %+v", rec)
	}
	if rep.Summary.Blocked != 1 {
		t.Fatalf("summary.blocked: %+v", rep.Summary)
	}
}

// Skip: o job some do cenário e o downstream bloqueia (dep nunca satisfaz).
func TestWhatIf_SkipBlocksDownstream(t *testing.T) {
	rep := WhatIf(wiDefs(), nil, "2026-07-10", nil,
		[]WhatIfChange{{DefID: "a", Skip: true}})

	if r := rowByID(t, rep, "a"); r.State != "skipped" || r.ScenRuns {
		t.Fatalf("a deveria ser skipped: %+v", r)
	}
	for _, id := range []string{"b", "c"} {
		if r := rowByID(t, rep, id); r.State != "blocked" {
			t.Fatalf("%s deveria bloquear com a fora da diária: %+v", id, r)
		}
	}
}

// Duração maior estoura SLA de deadline rio abaixo (newSlaBreaches conta só o novo).
func TestWhatIf_SLABreachOnScenario(t *testing.T) {
	defs := wiDefs()
	for i := range defs {
		if defs[i].ID == "c" {
			defs[i].SLA = &domain.SLASpec{DeadlineHM: "07:00"}
		}
	}
	durs := map[string]int64{"a": 10 * 60_000, "b": 10 * 60_000, "c": 10 * 60_000}
	// baseline: c termina 06:30 (ok). cenário: a dura 2h → c termina ~08:20 (estoura).
	rep := WhatIf(defs, nil, "2026-07-10", durs,
		[]WhatIfChange{{DefID: "a", DurationMs: 2 * 60 * 60_000}})

	c := rowByID(t, rep, "c")
	if c.SLABreachBase || !c.SLABreachScen {
		t.Fatalf("c deveria estourar SLA só no cenário: %+v", c)
	}
	if rep.Summary.NewSLABreaches != 1 {
		t.Fatalf("newSlaBreaches: %+v", rep.Summary)
	}
}

// Sem mudanças = cenário idêntico ao baseline (zero impactados).
func TestWhatIf_NoChangesNoImpact(t *testing.T) {
	rep := WhatIf(wiDefs(), nil, "2026-07-10", nil, nil)
	if rep.Summary.Impacted != 0 {
		t.Fatalf("sem mudanças não podia ter impacto: %+v", rep.Summary)
	}
	if rep.Summary.MakespanBaseMs != rep.Summary.MakespanScenMs {
		t.Fatal("makespans deveriam ser iguais")
	}
}
