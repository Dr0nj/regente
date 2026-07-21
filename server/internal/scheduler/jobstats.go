// Package scheduler — ADV-3 Statistics: estatísticas de execução POR DEFINITION.
//
// A visão "Statistics" do Control-M: totais por resultado, taxa de sucesso e a
// distribuição de duração (min/avg/p50/p90/max) sobre a janela recente. Complementa
// o PerfForecast (D-4), que foca em prever a PRÓXIMA execução — aqui é o retrato
// histórico pro operador (aba Stats do drawer).
package scheduler

import (
	"time"
)

const jobStatsWindow = 200 // últimas N execuções terminadas consideradas

type JobStats struct {
	DefID string `json:"defId"`
	// Contagem por resultado terminal (na janela).
	Runs        int     `json:"runs"`
	OK          int     `json:"ok"`
	NotOK       int     `json:"notok"`
	SuccessRate float64 `json:"successRate"` // OK / (OK+NOTOK); 0 quando sem runs

	// Distribuição de duração das execuções OK (ms) — consistente com o D-4,
	// que também mede performance só sobre OK (NOTOK distorce: falha rápida).
	MinMs int64 `json:"minMs"`
	AvgMs int64 `json:"avgMs"`
	P50Ms int64 `json:"p50Ms"`
	P90Ms int64 `json:"p90Ms"`
	MaxMs int64 `json:"maxMs"`

	// Última execução terminada.
	LastStatus     string     `json:"lastStatus,omitempty"`
	LastFinishedAt *time.Time `json:"lastFinishedAt,omitempty"`
	LastDurationMs int64      `json:"lastDurationMs,omitempty"`

	Window int `json:"window"` // teto de execuções consideradas
}

// JobStats — estatísticas da definition sobre as últimas execuções terminadas.
func (s *Scheduler) JobStats(defID string) JobStats {
	st := JobStats{DefID: defID, Window: jobStatsWindow}
	rows, err := s.db.Query(
		`SELECT status, started_at, finished_at FROM instances
		 WHERE definition_id=? AND status IN ('OK','NOTOK') AND finished_at IS NOT NULL
		 ORDER BY finished_at DESC LIMIT ?`, defID, jobStatsWindow)
	if err != nil {
		return st
	}
	defer rows.Close()

	var okDurs []int64
	first := true
	for rows.Next() {
		var status string
		var started, finished *time.Time
		if rows.Scan(&status, &started, &finished) != nil {
			continue
		}
		st.Runs++
		var durMs int64 = -1
		if started != nil && finished != nil {
			if ms := finished.Sub(*started).Milliseconds(); ms >= 0 {
				durMs = ms
			}
		}
		if first { // a query vem nova→velha: a primeira linha é a última execução
			first = false
			st.LastStatus = status
			if finished != nil {
				f := *finished
				st.LastFinishedAt = &f
			}
			if durMs >= 0 {
				st.LastDurationMs = durMs
			}
		}
		if status == "OK" {
			st.OK++
			if durMs >= 0 {
				okDurs = append(okDurs, durMs)
			}
		} else {
			st.NotOK++
		}
	}
	if err := rows.Err(); err != nil {
		// Estatística sobre janela incompleta mente (success rate/min/max tortos).
		return JobStats{DefID: defID, Window: jobStatsWindow}
	}
	if st.OK+st.NotOK > 0 {
		st.SuccessRate = float64(st.OK) / float64(st.OK+st.NotOK)
	}
	if len(okDurs) > 0 {
		var sum int64
		st.MinMs, st.MaxMs = okDurs[0], okDurs[0]
		for _, d := range okDurs {
			sum += d
			if d < st.MinMs {
				st.MinMs = d
			}
			if d > st.MaxMs {
				st.MaxMs = d
			}
		}
		st.AvgMs = sum / int64(len(okDurs))
		st.P50Ms, st.P90Ms = percentile(okDurs, 50), percentile(okDurs, 90)
	}
	return st
}
