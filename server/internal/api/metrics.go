// P17 — endpoint /metrics em formato texto Prometheus (sem dependência externa).
package api

import (
	"fmt"
	"net/http"
	"time"
)

func (s *server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	today := time.Now().Format("2006-01-02")

	fmt.Fprintln(w, "# HELP regente_up 1 se o server está no ar.")
	fmt.Fprintln(w, "# TYPE regente_up gauge")
	fmt.Fprintln(w, "regente_up 1")

	// Instances de hoje por status.
	fmt.Fprintln(w, "# HELP regente_instances Instances do order_date de hoje por status.")
	fmt.Fprintln(w, "# TYPE regente_instances gauge")
	rows, err := s.cfg.DB.Query(`SELECT status, COUNT(*) FROM instances WHERE order_date=? GROUP BY status`, today)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var status string
			var n int
			if rows.Scan(&status, &n) == nil {
				fmt.Fprintf(w, "regente_instances{status=%q} %d\n", status, n)
			}
		}
	}

	// Agentes online + capabilities.
	agents := s.cfg.Hub.OnlineAgents()
	fmt.Fprintln(w, "# HELP regente_agents_online Agentes conectados agora.")
	fmt.Fprintln(w, "# TYPE regente_agents_online gauge")
	fmt.Fprintf(w, "regente_agents_online %d\n", len(agents))

	// SLA breaches acumuladas.
	var breaches int
	_ = s.cfg.DB.QueryRow(`SELECT COUNT(*) FROM sla_breaches`).Scan(&breaches)
	fmt.Fprintln(w, "# HELP regente_sla_breaches_total Total de SLA breaches registradas.")
	fmt.Fprintln(w, "# TYPE regente_sla_breaches_total counter")
	fmt.Fprintf(w, "regente_sla_breaches_total %d\n", breaches)

	// Git: 1 se há drift entre local e remoto.
	drift := 0
	if s.cfg.Git != nil && s.cfg.Git.Status().Drift {
		drift = 1
	}
	fmt.Fprintln(w, "# HELP regente_git_drift 1 se o workspace está atrás do remoto.")
	fmt.Fprintln(w, "# TYPE regente_git_drift gauge")
	fmt.Fprintf(w, "regente_git_drift %d\n", drift)

	// R2 — watchdog do scheduler: idade do último ciclo (loop de scheduling vivo).
	if s.cfg.Scheduler != nil {
		age := -1.0
		if last := s.cfg.Scheduler.LastTick(); !last.IsZero() {
			age = time.Since(last).Seconds()
		}
		fmt.Fprintln(w, "# HELP regente_scheduler_last_tick_age_seconds Segundos desde o último ciclo do scheduler (-1 se nunca rodou).")
		fmt.Fprintln(w, "# TYPE regente_scheduler_last_tick_age_seconds gauge")
		fmt.Fprintf(w, "regente_scheduler_last_tick_age_seconds %.1f\n", age)
	}
}

// livez — R2/R3: liveness. Responde 200 enquanto o processo serve HTTP, e reporta
// a idade do último tick do scheduler (informativo). NÃO falha por tick velho: no
// modo -scheduler=external o tick vem de cron, e reprovar aqui causaria restart-loop.
// Para alertar em tick parado use o gauge regente_scheduler_last_tick_age_seconds (R7).
func (s *server) livez(w http.ResponseWriter, _ *http.Request) {
	age := -1.0
	if s.cfg.Scheduler != nil {
		if last := s.cfg.Scheduler.LastTick(); !last.IsZero() {
			age = time.Since(last).Seconds()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "{\"alive\":true,\"schedulerLastTickAgeSeconds\":%.1f}\n", age)
}
