// Package scheduler — F21 Forecast / dry-run.
//
// Simula em RAM o que aconteceria no order_date dado: quais defs seriam
// materializadas (respeitando calendar), em que ordem topológica seriam
// executadas (deps), quem seria bloqueado por resources/conditions, e
// quem violaria SLA com a duração esperada.
//
// Não toca DB nem hub. 100% pure function.
package scheduler

import (
	"sort"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

type ForecastJob struct {
	DefID          string    `json:"defId"`
	Label          string    `json:"label"`
	Team           string    `json:"team"`
	StartAt        time.Time `json:"startAt"`
	EndAt          time.Time `json:"endAt"`
	Eligible       bool      `json:"eligible"`
	Reason         string    `json:"reason,omitempty"` // "no calendar match", "blocked by deps", etc.
	Wave           int       `json:"wave"`             // ordem topológica (0 = raiz)
	WouldBreachSLA bool      `json:"wouldBreachSla"`
}

type ForecastReport struct {
	OrderDate string         `json:"orderDate"`
	Jobs      []ForecastJob  `json:"jobs"`
	Resources map[string]int `json:"peakResourceUsage,omitempty"`
}

// Forecast simula execution para `orderDate`.
func Forecast(defs []domain.JobDefinition, calendars map[string]*domain.Calendar, orderDate string) ForecastReport {
	od, _ := time.Parse("2006-01-02", orderDate)

	// 1. Filtrar elegíveis (calendar)
	type entry struct {
		def      domain.JobDefinition
		eligible bool
		reason   string
	}
	entries := map[string]*entry{}
	for _, d := range defs {
		e := &entry{def: d, eligible: d.Schedule.Enabled}
		if !d.Schedule.Enabled {
			e.reason = "schedule.enabled=false"
		}
		if d.Calendar != "" {
			cal := calendars[d.Calendar]
			if !IsEligibleDate(cal, od) {
				e.eligible = false
				e.reason = "calendar " + d.Calendar + " excludes this date"
			}
		}
		entries[d.ID] = e
	}

	// 2. Topological wave assignment
	wave := map[string]int{}
	var assign func(string) int
	visiting := map[string]bool{}
	assign = func(id string) int {
		if w, ok := wave[id]; ok {
			return w
		}
		if visiting[id] {
			// ciclo — corta
			wave[id] = 0
			return 0
		}
		visiting[id] = true
		max := 0
		e, ok := entries[id]
		if ok {
			for _, u := range e.def.Upstream {
				w := assign(u.From)
				if w+1 > max {
					max = w + 1
				}
			}
		}
		wave[id] = max
		visiting[id] = false
		return max
	}
	for id := range entries {
		assign(id)
	}

	// 3. Estimar tempos (assume 5min se sla.expectedDurationMin não setado)
	jobs := []ForecastJob{}
	startBase := time.Date(od.Year(), od.Month(), od.Day(), 6, 0, 0, 0, time.Local)
	for id, e := range entries {
		dur := 5
		if e.def.SLA != nil && e.def.SLA.ExpectedDurationMin > 0 {
			dur = e.def.SLA.ExpectedDurationMin
		}
		w := wave[id]
		start := startBase.Add(time.Duration(w*dur) * time.Minute)
		end := start.Add(time.Duration(dur) * time.Minute)
		breach := false
		if e.def.SLA != nil && e.def.SLA.DeadlineHM != "" {
			// só sinaliza se end > deadline
			// (parsing simples: HH:MM no mesmo dia)
			hh, mm := 0, 0
			_, _ = fmtScanHM(e.def.SLA.DeadlineHM, &hh, &mm)
			deadline := time.Date(od.Year(), od.Month(), od.Day(), hh, mm, 0, 0, time.Local)
			breach = end.After(deadline)
		}
		jobs = append(jobs, ForecastJob{
			DefID: id, Label: e.def.Label, Team: e.def.Team,
			StartAt: start, EndAt: end,
			Eligible: e.eligible, Reason: e.reason, Wave: w,
			WouldBreachSLA: breach,
		})
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].Wave != jobs[j].Wave {
			return jobs[i].Wave < jobs[j].Wave
		}
		return jobs[i].DefID < jobs[j].DefID
	})

	// 4. Peak resources (simplificado: sum por wave)
	peak := map[string]int{}
	byWave := map[int]map[string]int{}
	for _, j := range jobs {
		if !j.Eligible {
			continue
		}
		e := entries[j.DefID]
		if byWave[j.Wave] == nil {
			byWave[j.Wave] = map[string]int{}
		}
		for r, q := range e.def.Resources {
			byWave[j.Wave][r] += q
		}
	}
	for _, m := range byWave {
		for r, q := range m {
			if q > peak[r] {
				peak[r] = q
			}
		}
	}
	return ForecastReport{OrderDate: orderDate, Jobs: jobs, Resources: peak}
}

// helper minimal sscanf "HH:MM"
func fmtScanHM(s string, hh, mm *int) (int, error) {
	if len(s) < 4 {
		return 0, nil
	}
	*hh = int(s[0]-'0')*10 + int(s[1]-'0')
	*mm = int(s[3]-'0')*10 + int(s[4]-'0')
	return 2, nil
}
