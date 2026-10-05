package api

import (
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Janela móvel de 1h, no máximo 10000 tentativas mais recentes. Quantis não são counters.
func (s *server) capacityMetrics(w http.ResponseWriter) {
	stats := s.cfg.DB.Raw().Stats()
	fmt.Fprintf(w, "regente_db_open_connections %d\nregente_db_in_use_connections %d\nregente_db_wait_total %d\nregente_db_wait_seconds_total %.6f\n", stats.OpenConnections, stats.InUse, stats.WaitCount, stats.WaitDuration.Seconds())
	if sch := s.cfg.Scheduler; sch != nil {
		p := sch.TickProgress()
		age := -1.0
		if !p.CompletedAt.IsZero() {
			age = time.Since(p.CompletedAt).Seconds()
		}
		fmt.Fprintf(w, "regente_scheduler_completed_tick_age_seconds %.3f\nregente_scheduler_tick_duration_seconds %.6f\nregente_scheduler_ticks_total{outcome=%q} %d\nregente_scheduler_ticks_total{outcome=%q} %d\nregente_scheduler_ticks_total{outcome=%q} %d\nregente_scheduler_ticks_total{outcome=%q} %d\nregente_scheduler_ticks_total{outcome=%q} %d\n", age, p.Duration.Seconds(), "attempted", p.Attempts, "completed", p.Completed, "failed", p.Failed, "overlap", p.Overlap, "follower", p.Follower)
	}
	var oldest sql.NullInt64
	if err := s.cfg.DB.QueryRow("SELECT MIN(created_at) FROM execution_outbox WHERE state IN ('pending','leased','paused')").Scan(&oldest); err != nil {
		fmt.Fprintln(w, "regente_capacity_metrics_available 0")
		return
	}
	age := 0.0
	if oldest.Valid {
		age = max(0, float64(time.Now().UnixMilli()-oldest.Int64)/1000)
	}
	fmt.Fprintf(w, "regente_execution_oldest_outbox_seconds %.3f\n", age)
	// Idade antes do planejamento: tentativas incompletas não podem sumir do SLO.
	var waitingReady string
	waitingAge := 0.0
	errReady := s.cfg.DB.QueryRow("SELECT ev.message FROM instance_events ev JOIN instances i ON i.id=ev.instance_id WHERE ev.kind='eligible' AND i.status='WAITING' AND NOT EXISTS(SELECT 1 FROM runtime_orders r WHERE r.instance_id=i.id) ORDER BY ev.ts LIMIT 1").Scan(&waitingReady)
	if errReady != nil && errReady != sql.ErrNoRows {
		fmt.Fprintln(w, "regente_capacity_metrics_available 0")
		return
	}
	if errReady == nil {
		ready, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(waitingReady, "Business gates passed at "))
		if err != nil {
			fmt.Fprintln(w, "regente_capacity_metrics_available 0")
			return
		}
		waitingAge = max(0, time.Since(ready).Seconds())
	}
	fmt.Fprintf(w, "regente_execution_oldest_eligible_wait_seconds %.3f\n", waitingAge)
	var slots, active int
	if err := s.cfg.DB.QueryRow("SELECT COALESCE(SUM(slots),0) FROM execution_agent_capacity WHERE available=1 AND last_seen>?", time.Now().Add(-15*time.Second).UnixMilli()).Scan(&slots); err != nil {
		fmt.Fprintln(w, "regente_capacity_metrics_available 0")
		return
	}
	if err := s.cfg.DB.QueryRow("SELECT COUNT(*) FROM execution_attempts WHERE state NOT IN ('succeeded','failed','cancelled')").Scan(&active); err != nil {
		fmt.Fprintln(w, "regente_capacity_metrics_available 0")
		return
	}
	fmt.Fprintf(w, "regente_execution_agent_slots %d\nregente_execution_active_attempts %d\n", slots, active)
	stages := map[string][]float64{"ready_to_planned": {}, "planned_to_accepted": {}, "accepted_to_started": {}, "started_to_finished": {}, "ready_to_started": {}}
	cutoff := time.Now().Add(-time.Hour).UnixMilli()
	rows, err := s.cfg.DB.Query("SELECT a.attempt,a.created_at,a.accepted_at,a.started_at,a.finished_at,(SELECT ev.message FROM instance_events ev WHERE ev.instance_id=r.instance_id AND ev.kind='eligible' ORDER BY ev.ts LIMIT 1) FROM execution_attempts a JOIN runtime_orders r ON r.order_id=a.order_id WHERE a.created_at>=? ORDER BY a.created_at DESC LIMIT 10000", cutoff)
	if err != nil {
		fmt.Fprintln(w, "regente_capacity_metrics_available 0")
		return
	}
	sampled := 0
	for rows.Next() {
		var number int
		var planned, accepted, started, finished int64
		var readyRaw sql.NullString
		if err = rows.Scan(&number, &planned, &accepted, &started, &finished, &readyRaw); err != nil {
			break
		}
		sampled++
		add := func(stage string, a, b int64) {
			if a > 0 && b >= a {
				stages[stage] = append(stages[stage], float64(b-a)/1000)
			}
		}
		if number == 1 && readyRaw.Valid {
			ready, parseErr := time.Parse(time.RFC3339Nano, strings.TrimPrefix(readyRaw.String, "Business gates passed at "))
			if parseErr != nil {
				err = parseErr
				break
			}
			add("ready_to_planned", ready.UnixMilli(), planned)
			add("ready_to_started", ready.UnixMilli(), started)
		}
		add("planned_to_accepted", planned, accepted)
		add("accepted_to_started", accepted, started)
		add("started_to_finished", started, finished)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fmt.Fprintln(w, "regente_capacity_metrics_available 0")
		return
	}
	fmt.Fprintf(w, "regente_capacity_metrics_available 1\nregente_execution_latency_sampled_attempts %d\n", sampled)
	// Observações incompletas não entram nos quantis; filas/idade continuam explícitas.
	for _, name := range []string{"ready_to_planned", "planned_to_accepted", "accepted_to_started", "started_to_finished", "ready_to_started"} {
		values := stages[name]
		sort.Float64s(values)
		fmt.Fprintf(w, "regente_execution_stage_observations{stage=%q,window=%q} %d\n", name, "1h_last_10000", len(values))
		budget := 0.0
		if name == "planned_to_accepted" {
			budget = 5
		}
		if name == "ready_to_started" {
			budget = 10
		}
		if budget > 0 {
			breaches := 0
			for _, value := range values {
				if value > budget {
					breaches++
				}
			}
			fmt.Fprintf(w, "regente_execution_stage_budget_breaches{stage=%q,window=%q,threshold_seconds=%q} %d\n", name, "1h_last_10000", fmt.Sprint(budget), breaches)
		}
		if len(values) > 0 {
			for _, q := range []float64{0.95, 0.99} {
				index := int(math.Ceil(float64(len(values))*q)) - 1
				fmt.Fprintf(w, "regente_execution_stage_seconds{stage=%q,window=%q,quantile=%q} %.6f\n", name, "1h_last_10000", fmt.Sprint(q), values[index])
			}
		}
	}
}
