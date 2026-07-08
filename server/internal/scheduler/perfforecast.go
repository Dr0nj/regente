// Package scheduler — D-4 Performance forecasting.
//
// Previsão de duração POR HISTÓRICO (determinística, sem IA): últimas
// execuções OK da definition → p50/p90, tendência (regressão linear sobre a
// ordem das execuções) e previsão da próxima. Se a instance está RUNNING,
// projeta o ETA (p50 − decorrido). Alimenta o gráfico do drawer no Monitoring
// e as barras "previstas" da Timeline (D-12).
package scheduler

import (
	"sort"
	"time"
)

type DurationSample struct {
	OrderDate  string `json:"orderDate"`
	DurationMs int64  `json:"durationMs"`
}

type PerfForecast struct {
	DefID   string           `json:"defId"`
	Samples []DurationSample `json:"samples"` // cronológicas (velha → nova)
	P50Ms   int64            `json:"p50Ms"`
	P90Ms   int64            `json:"p90Ms"`
	// TrendMsPerRun — inclinação da regressão linear (ms por execução).
	// Positiva e relevante (>2% do p50) ⇒ o job está ficando mais lento.
	TrendMsPerRun float64 `json:"trendMsPerRun"`
	Slower        bool    `json:"slower"`
	NextMs        int64   `json:"nextMs"` // previsão da próxima duração
	// EtaMs — só quando há uma instance RUNNING agora: estimativa do que falta
	// (nextMs − decorrido; nunca negativa — job "atrasado" mostra 0 e o front
	// sinaliza estouro comparando elapsed com nextMs).
	EtaMs *int64 `json:"etaMs,omitempty"`
}

const perfSampleWindow = 30

// PerfForecast calcula a previsão da definition a partir do histórico OK.
func (s *Scheduler) PerfForecast(defID string) PerfForecast {
	pf := PerfForecast{DefID: defID, Samples: []DurationSample{}}
	rows, err := s.db.Query(
		`SELECT order_date, started_at, finished_at FROM instances
		 WHERE definition_id=? AND status='OK' AND started_at IS NOT NULL AND finished_at IS NOT NULL
		 ORDER BY finished_at DESC LIMIT ?`, defID, perfSampleWindow)
	if err != nil {
		return pf
	}
	for rows.Next() {
		var od string
		var st, fi time.Time
		if rows.Scan(&od, &st, &fi) == nil {
			if ms := fi.Sub(st).Milliseconds(); ms >= 0 {
				pf.Samples = append(pf.Samples, DurationSample{OrderDate: od, DurationMs: ms})
			}
		}
	}
	rows.Close()
	// query veio nova→velha; série temporal quer velha→nova.
	for i, j := 0, len(pf.Samples)-1; i < j; i, j = i+1, j-1 {
		pf.Samples[i], pf.Samples[j] = pf.Samples[j], pf.Samples[i]
	}
	if len(pf.Samples) == 0 {
		return pf
	}

	durs := make([]int64, len(pf.Samples))
	for i, s := range pf.Samples {
		durs[i] = s.DurationMs
	}
	pf.P50Ms, pf.P90Ms = percentile(durs, 50), percentile(durs, 90)
	pf.TrendMsPerRun = slope(durs)
	pf.Slower = pf.P50Ms > 0 && pf.TrendMsPerRun > float64(pf.P50Ms)*0.02

	// previsão: p50 + tendência projetada 1 passo à frente (clamp em >=0).
	next := float64(pf.P50Ms) + pf.TrendMsPerRun
	if next < 0 {
		next = 0
	}
	pf.NextMs = int64(next)

	// ETA da execução em curso, se houver.
	var startedAt time.Time
	err = s.db.QueryRow(
		`SELECT started_at FROM instances
		 WHERE definition_id=? AND status='RUNNING' AND started_at IS NOT NULL
		 ORDER BY started_at DESC LIMIT 1`, defID).Scan(&startedAt)
	if err == nil {
		remaining := pf.NextMs - time.Since(startedAt).Milliseconds()
		if remaining < 0 {
			remaining = 0
		}
		pf.EtaMs = &remaining
	}
	return pf
}

// DayDurations — p50 histórico por definition para as instances de um dia
// (as barras "previstas" da Timeline). Uma query na janela de `lookbackDays`,
// agregação em Go — sem SQL de percentil dialect-específico.
func (s *Scheduler) DayDurations(date string, lookbackDays int) map[string]int64 {
	from := date
	if t, err := time.Parse("2006-01-02", date); err == nil {
		from = t.AddDate(0, 0, -lookbackDays).Format("2006-01-02")
	}
	rows, err := s.db.Query(
		`SELECT definition_id, started_at, finished_at FROM instances
		 WHERE order_date >= ? AND order_date <= ? AND status='OK'
		   AND started_at IS NOT NULL AND finished_at IS NOT NULL`, from, date)
	if err != nil {
		return map[string]int64{}
	}
	byDef := map[string][]int64{}
	for rows.Next() {
		var id string
		var st, fi time.Time
		if rows.Scan(&id, &st, &fi) == nil {
			if ms := fi.Sub(st).Milliseconds(); ms >= 0 {
				byDef[id] = append(byDef[id], ms)
			}
		}
	}
	rows.Close()
	out := make(map[string]int64, len(byDef))
	for id, durs := range byDef {
		out[id] = percentile(durs, 50)
	}
	return out
}

func percentile(durs []int64, p int) int64 {
	if len(durs) == 0 {
		return 0
	}
	sorted := append([]int64{}, durs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (len(sorted) - 1) * p / 100
	return sorted[idx]
}

// slope — inclinação da regressão linear y=a+bx sobre a série (x = índice).
func slope(ys []int64) float64 {
	n := float64(len(ys))
	if n < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for i, y := range ys {
		x := float64(i)
		sumX += x
		sumY += float64(y)
		sumXY += x * float64(y)
		sumXX += x * x
	}
	den := n*sumXX - sumX*sumX
	if den == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / den
}
