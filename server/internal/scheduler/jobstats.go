// Package scheduler — ADV-3 Statistics: estatísticas de execução POR DEFINITION.
//
// A visão "Statistics" do Control-M: as últimas execuções com início/fim/tempo de
// execução, totais por resultado, taxa de sucesso e a distribuição de duração
// (min/avg/p50/p90/max). Complementa o PerfForecast (D-4), que foca em prever a
// PRÓXIMA execução — aqui é o retrato histórico pro operador (aba Stats do drawer).
//
// ST-1 (2026-08-05) — a fonte é `instance_runs` (uma linha por EXECUÇÃO REAL),
// não mais o par started_at/finished_at da instance. Ver runs.go para o porquê:
// aquele par é mutado por transições que não são execução (Set OK de quem não
// rodou, retry/cyclic sobrescrevendo a tentativa anterior) e inflava média/máximo.
package scheduler

import (
	"database/sql"
	"time"
)

const (
	jobStatsWindow = 200 // últimas N execuções terminadas consideradas
	jobStatsRecent = 10  // execuções detalhadas devolvidas (lista início/fim)
)

// RunSample — uma execução terminada, como o operador a vê na lista: quando
// entrou em RUNNING, quando terminou e quanto durou de fato.
type RunSample struct {
	InstanceID string    `json:"instanceId"`
	OrderDate  string    `json:"orderDate"` // ODAT (origem, se carregada)
	Attempt    int       `json:"attempt"`
	Status     string    `json:"status"` // OK | NOTOK
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	DurationMs int64     `json:"durationMs"`
	ExitCode   *int      `json:"exitCode,omitempty"`
}

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

	// Últimas execuções (nova → velha), até jobStatsRecent.
	Recent []RunSample `json:"recent"`

	// Última execução terminada.
	LastStatus     string     `json:"lastStatus,omitempty"`
	LastFinishedAt *time.Time `json:"lastFinishedAt,omitempty"`
	LastDurationMs int64      `json:"lastDurationMs,omitempty"`

	Window int `json:"window"` // teto de execuções consideradas
}

// JobStats — estatísticas da definition sobre as últimas execuções terminadas.
func (s *Scheduler) JobStats(defID string) JobStats {
	st := JobStats{DefID: defID, Window: jobStatsWindow, Recent: []RunSample{}}
	rows, err := s.db.Query(
		`SELECT instance_id, order_date, attempt, status, started_at, finished_at, exit_code
		   FROM instance_runs
		  WHERE definition_id=? AND status IN ('OK','NOTOK') AND finished_at IS NOT NULL
		  ORDER BY finished_at DESC, id DESC LIMIT ?`, defID, jobStatsWindow)
	if err != nil {
		return st
	}
	defer rows.Close()

	var okDurs []int64
	for rows.Next() {
		var r RunSample
		var exit sql.NullInt64
		if rows.Scan(&r.InstanceID, &r.OrderDate, &r.Attempt, &r.Status, &r.StartedAt, &r.FinishedAt, &exit) != nil {
			continue
		}
		r.DurationMs = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
		if r.DurationMs < 0 {
			r.DurationMs = 0
		}
		if exit.Valid {
			code := int(exit.Int64)
			r.ExitCode = &code
		}
		st.Runs++
		if len(st.Recent) < jobStatsRecent {
			st.Recent = append(st.Recent, r)
		}
		if st.Runs == 1 { // a query vem nova→velha: a primeira linha é a última execução
			st.LastStatus = r.Status
			f := r.FinishedAt
			st.LastFinishedAt = &f
			st.LastDurationMs = r.DurationMs
		}
		if r.Status == "OK" {
			st.OK++
			okDurs = append(okDurs, r.DurationMs)
		} else {
			st.NotOK++
		}
	}
	if err := rows.Err(); err != nil {
		// Estatística sobre janela incompleta mente (success rate/min/max tortos).
		return JobStats{DefID: defID, Window: jobStatsWindow, Recent: []RunSample{}}
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
