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

	// 1. Filtrar elegíveis — MESMA regra do RunDaily (fonte única): enabled +
	// IsScheduledOn (frequência + monthsOfYear + calendars include/exclude +
	// shift). Antes o Forecast só olhava o calendar LEGADO (d.Calendar) via
	// IsEligibleDate e ignorava frequency/months/multi-calendar/shift — um job
	// "só segundas" aparecia elegível todo dia, divergindo do que a daily de fato
	// materializava. Agora o dry-run casa o gating real (CTM-6).
	store := mapCalLookup(calendars)
	type entry struct {
		def      domain.JobDefinition
		eligible bool
		reason   string
	}
	entries := map[string]*entry{}
	for _, d := range defs {
		e := &entry{def: d, eligible: d.Schedule.Enabled}
		switch {
		case !d.Schedule.Enabled:
			e.reason = "schedule.enabled=false"
		case !IsScheduledOn(d, od, store):
			e.eligible = false
			e.reason = scheduleReason(d, od)
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

// mapCalLookup adapta um mapa nome→calendar ao calLookup que IsScheduledOn usa
// (o mesmo contrato do CalendarStore). Mapa nil → Get devolve (nil,nil), então o
// gating cai só na frequência, idêntico ao RunDaily sem calStore.
type mapCalLookup map[string]*domain.Calendar

func (m mapCalLookup) Get(name string) (*domain.Calendar, error) { return m[name], nil }

// scheduleReason descreve, em linguagem de operador, por que a def NÃO foi
// agendada em `date` (quando IsScheduledOn deu false). É best-effort — aponta o
// primeiro filtro que barra — para o painel de Forecast explicar a ausência.
func scheduleReason(d domain.JobDefinition, date time.Time) string {
	s := d.Schedule
	if len(s.MonthsOfYear) > 0 && !containsInt(s.MonthsOfYear, int(date.Month())) {
		return "mês fora de monthsOfYear"
	}
	switch s.Frequency {
	case "weekly":
		if len(s.DaysOfWeek) == 0 {
			return "weekly sem daysOfWeek (schedule incompleto)"
		}
		return "weekday fora de daysOfWeek"
	case "monthly":
		if len(s.DaysOfMonth) == 0 {
			return "monthly sem daysOfMonth (schedule incompleto)"
		}
		return "dia-do-mês fora de daysOfMonth"
	case "businessday":
		return "não é o N-ésimo dia útil configurado"
	case "advanced":
		return "não casa a regra avançada '" + s.AdvancedRule + "'"
	}
	// Frequência casou: sobrou o calendar (include/exclude) ou o shift barrando.
	return "calendar/shift exclui esta data"
}

// ForecastRange roda o Forecast por `days` dias a partir de `from` (inclusive) —
// a visão "≥1 semana à frente" do Control-M. Cada dia passa pela MESMA regra de
// gating do RunDaily (via Forecast → IsScheduledOn): calendars, frequência,
// meses, deps (ondas topológicas) e pico de recursos. days é limitado a [1,366].
func ForecastRange(defs []domain.JobDefinition, calendars map[string]*domain.Calendar, from string, days int) []ForecastReport {
	if days < 1 {
		days = 1
	}
	if days > 366 {
		days = 366
	}
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return nil
	}
	out := make([]ForecastReport, 0, days)
	for i := 0; i < days; i++ {
		od := start.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, Forecast(defs, calendars, od))
	}
	return out
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
