// Package scheduler — ADV-3 What-If: simulação de cenário sobre a diária.
//
// "E se o job X atrasar 40min / demorar o dobro / FALHAR / não rodar?" —
// projeta a diária do orderDate duas vezes (baseline e cenário) e devolve o
// impacto downstream por job: início/fim projetados, atraso, bloqueio (deps
// impossíveis) e violação de SLA. Mesmo ethos do Forecast (F21): função PURA,
// sem DB nem hub — a API injeta defs, calendars e as durações REAIS (p50 do
// histórico, D-4) e o simulador só propaga.
//
// Semântica de dependência = a do engine (edgeState, fonte única de conceito):
// TODAS as arestas precisam satisfazer (AND); on-success exige upstream OK,
// on-failure exige upstream NOTOK (job de recovery NÃO roda no baseline e
// PASSA a rodar no cenário de falha), on-complete/always exigem só terminar.
// Upstream fora da diária (não elegível/skip) bloqueia o sucessor — igual ao
// "ainda não ordenado" do Explain.
//
// Aproximações (documentadas de propósito): âncora T0 = 00:00 do orderDate
// (runAt manda quando existe); duração = p50 histórico → sla.expectedDurationMin
// → 5min (default do Forecast); cyclic conta como 1 execução; recursos/conditions
// globais fora do modelo (o que importa aqui é a PROPAGAÇÃO por deps).
package scheduler

import (
	"sort"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// WhatIfChange — uma mutação de cenário sobre um job.
type WhatIfChange struct {
	DefID      string `json:"defId"`
	DelayMin   int    `json:"delayMin,omitempty"`   // atrasa o INÍCIO em N minutos
	DurationMs int64  `json:"durationMs,omitempty"` // override da duração (0 = mantém)
	Fail       bool   `json:"fail,omitempty"`       // termina NOTOK (dispara on-failure, bloqueia on-success)
	Skip       bool   `json:"skip,omitempty"`       // não roda (como se não fosse ordenado)
}

// WhatIfRow — um job da diária nas duas projeções.
type WhatIfRow struct {
	DefID string `json:"defId"`
	Label string `json:"label"`
	Team  string `json:"team"`
	Wave  int    `json:"wave"`

	// Baseline (todo mundo OK). Ausentes quando o job não roda no baseline
	// (ex.: só tem aresta on-failure).
	BaseRuns  bool       `json:"baseRuns"`
	BaseStart *time.Time `json:"baseStart,omitempty"`
	BaseEnd   *time.Time `json:"baseEnd,omitempty"`

	// Cenário.
	ScenRuns   bool       `json:"scenRuns"`
	ScenStart  *time.Time `json:"scenStart,omitempty"`
	ScenEnd    *time.Time `json:"scenEnd,omitempty"`
	ScenStatus string     `json:"scenStatus,omitempty"` // "OK" | "NOTOK" (fail simulado)

	// Derivados pro operador.
	DeltaMs        int64  `json:"deltaMs"`        // fim cenário − fim baseline (>0 = atrasou)
	State          string `json:"state"`          // unchanged|delayed|earlier|blocked|skipped|fails|starts-running|not-run
	SLABreachBase  bool   `json:"slaBreachBase"`  // já estourava no baseline
	SLABreachScen  bool   `json:"slaBreachScen"`  // estoura no cenário
	Impacted       bool   `json:"impacted"`       // qualquer diferença baseline→cenário
	ChangeInjected bool   `json:"changeInjected"` // este job é um dos mutados do cenário
}

type WhatIfSummary struct {
	Total          int   `json:"total"`
	Impacted       int   `json:"impacted"`
	Blocked        int   `json:"blocked"`
	NewSLABreaches int   `json:"newSlaBreaches"`
	MakespanBaseMs int64 `json:"makespanBaseMs"` // T0 → último fim projetado
	MakespanScenMs int64 `json:"makespanScenMs"`
}

type WhatIfReport struct {
	OrderDate string        `json:"orderDate"`
	Rows      []WhatIfRow   `json:"rows"`
	Summary   WhatIfSummary `json:"summary"`
}

// projJob — resultado de uma projeção (baseline ou cenário) pra um job.
type projJob struct {
	runs   bool
	start  time.Time
	end    time.Time
	status string // OK | NOTOK (só quando runs)
}

// WhatIf — simula a diária de orderDate com e sem as mudanças.
// durations = p50 REAL por definition (ms) — DayDurations (D-4); pode ser nil.
func WhatIf(defs []domain.JobDefinition, calendars map[string]*domain.Calendar,
	orderDate string, durations map[string]int64, changes []WhatIfChange) WhatIfReport {

	od, _ := time.Parse("2006-01-02", orderDate)
	t0 := time.Date(od.Year(), od.Month(), od.Day(), 0, 0, 0, 0, time.Local)
	store := mapCalLookup(calendars)

	// 1) Elegíveis do dia — MESMA regra do RunDaily/Forecast (fonte única).
	byID := map[string]*domain.JobDefinition{}
	for i := range defs {
		d := &defs[i]
		if d.Schedule.Enabled && IsScheduledOn(*d, od, store) {
			byID[d.ID] = d
		}
	}

	chg := map[string]*WhatIfChange{}
	for i := range changes {
		if _, ok := byID[changes[i].DefID]; ok {
			chg[changes[i].DefID] = &changes[i]
		}
	}

	// 2) Duração projetada: p50 real → sla.expectedDurationMin → 5min.
	durOf := func(d *domain.JobDefinition) time.Duration {
		if ms, ok := durations[d.ID]; ok && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
		if d.SLA != nil && d.SLA.ExpectedDurationMin > 0 {
			return time.Duration(d.SLA.ExpectedDurationMin) * time.Minute
		}
		return 5 * time.Minute
	}

	// 3) Uma projeção = DFS memoizado sobre as deps (AND, condição por aresta).
	project := func(scenario bool) map[string]projJob {
		out := map[string]projJob{}
		visiting := map[string]bool{}
		var visit func(id string) projJob
		visit = func(id string) projJob {
			if p, ok := out[id]; ok {
				return p
			}
			if visiting[id] {
				out[id] = projJob{} // ciclo — corta (como o Forecast)
				return out[id]
			}
			visiting[id] = true
			defer func() { visiting[id] = false }()

			d, ok := byID[id]
			if !ok {
				out[id] = projJob{} // upstream fora da diária = nunca satisfaz
				return out[id]
			}
			c := chg[id]
			if scenario && c != nil && c.Skip {
				out[id] = projJob{} // não roda no cenário
				return out[id]
			}

			// Deps: TODAS as arestas precisam satisfazer.
			depReady := true
			depEnd := time.Time{}
			for _, u := range d.Upstream {
				up := visit(u.From)
				sat := false
				if up.runs {
					switch u.Condition {
					case domain.CondOnSuccess, "":
						sat = up.status == string(domain.StatusOK)
					case domain.CondOnFailure:
						sat = up.status == string(domain.StatusNotOK)
					case domain.CondOnComplete, domain.CondAlways:
						sat = true
					}
				}
				if !sat {
					depReady = false
					break
				}
				if up.end.After(depEnd) {
					depEnd = up.end
				}
			}
			if !depReady {
				out[id] = projJob{}
				return out[id]
			}

			// Início: âncora própria (runAt ou T0) vs fim do último upstream.
			start := t0
			if hm := d.Schedule.RunAt; len(hm) == 5 {
				hh, mm := 0, 0
				_, _ = fmtScanHM(hm, &hh, &mm)
				start = time.Date(od.Year(), od.Month(), od.Day(), hh, mm, 0, 0, time.Local)
			}
			if depEnd.After(start) {
				start = depEnd
			}
			dur := durOf(d)
			status := string(domain.StatusOK)
			if scenario && c != nil {
				if c.DelayMin > 0 {
					start = start.Add(time.Duration(c.DelayMin) * time.Minute)
				}
				if c.DurationMs > 0 {
					dur = time.Duration(c.DurationMs) * time.Millisecond
				}
				if c.Fail {
					status = string(domain.StatusNotOK)
				}
			}
			out[id] = projJob{runs: true, start: start, end: start.Add(dur), status: status}
			return out[id]
		}
		for id := range byID {
			visit(id)
		}
		return out
	}

	base := project(false)
	scen := project(true)

	// 4) Ondas topológicas (ordenação de leitura, igual ao Forecast).
	wave := map[string]int{}
	visitingW := map[string]bool{}
	var assignWave func(string) int
	assignWave = func(id string) int {
		if w, ok := wave[id]; ok {
			return w
		}
		if visitingW[id] {
			wave[id] = 0
			return 0
		}
		visitingW[id] = true
		defer func() { visitingW[id] = false }()
		max := 0
		if d, ok := byID[id]; ok {
			for _, u := range d.Upstream {
				if w := assignWave(u.From) + 1; w > max {
					max = w
				}
			}
		}
		wave[id] = max
		return max
	}

	slaBreach := func(d *domain.JobDefinition, p projJob) bool {
		if !p.runs || d.SLA == nil || d.SLA.DeadlineHM == "" {
			return false
		}
		hh, mm := 0, 0
		_, _ = fmtScanHM(d.SLA.DeadlineHM, &hh, &mm)
		deadline := time.Date(od.Year(), od.Month(), od.Day(), hh, mm, 0, 0, time.Local)
		return p.end.After(deadline)
	}

	rep := WhatIfReport{OrderDate: orderDate, Rows: []WhatIfRow{}}
	for id, d := range byID {
		b, sc := base[id], scen[id]
		row := WhatIfRow{
			DefID: id, Label: d.Label, Team: d.Team, Wave: assignWave(id),
			BaseRuns: b.runs, ScenRuns: sc.runs,
			SLABreachBase: slaBreach(d, b), SLABreachScen: slaBreach(d, sc),
		}
		if chg[id] != nil {
			row.ChangeInjected = true
		}
		if b.runs {
			bs, be := b.start, b.end
			row.BaseStart, row.BaseEnd = &bs, &be
		}
		if sc.runs {
			ss, se := sc.start, sc.end
			row.ScenStart, row.ScenEnd = &ss, &se
			row.ScenStatus = sc.status
		}
		switch {
		case b.runs && !sc.runs:
			if c := chg[id]; c != nil && c.Skip {
				row.State = "skipped"
			} else {
				row.State = "blocked" // deps ficaram impossíveis no cenário
			}
		case !b.runs && sc.runs:
			row.State = "starts-running" // ex.: recovery on-failure destravado
		case !b.runs && !sc.runs:
			row.State = "not-run"
		case sc.status == string(domain.StatusNotOK):
			row.State = "fails"
			row.DeltaMs = sc.end.Sub(b.end).Milliseconds()
		default:
			row.DeltaMs = sc.end.Sub(b.end).Milliseconds()
			switch {
			case row.DeltaMs > 0:
				row.State = "delayed"
			case row.DeltaMs < 0:
				row.State = "earlier"
			default:
				row.State = "unchanged"
			}
		}
		row.Impacted = row.State != "unchanged" && row.State != "not-run"
		rep.Rows = append(rep.Rows, row)

		if b.runs {
			if ms := b.end.Sub(t0).Milliseconds(); ms > rep.Summary.MakespanBaseMs {
				rep.Summary.MakespanBaseMs = ms
			}
		}
		if sc.runs {
			if ms := sc.end.Sub(t0).Milliseconds(); ms > rep.Summary.MakespanScenMs {
				rep.Summary.MakespanScenMs = ms
			}
		}
		rep.Summary.Total++
		if row.Impacted {
			rep.Summary.Impacted++
		}
		if row.State == "blocked" {
			rep.Summary.Blocked++
		}
		if row.SLABreachScen && !row.SLABreachBase {
			rep.Summary.NewSLABreaches++
		}
	}

	// Impactados primeiro (maior atraso no topo), depois onda/id — leitura de operador.
	sort.Slice(rep.Rows, func(i, j int) bool {
		a, b := rep.Rows[i], rep.Rows[j]
		if a.Impacted != b.Impacted {
			return a.Impacted
		}
		if a.DeltaMs != b.DeltaMs {
			return a.DeltaMs > b.DeltaMs
		}
		if a.Wave != b.Wave {
			return a.Wave < b.Wave
		}
		return a.DefID < b.DefID
	})
	return rep
}
