// P17 — endpoint /metrics em formato texto Prometheus (sem dependência externa).
package api

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// readyTickStaleSeconds — acima disso o tick do scheduler é reportado como "stale"
// no /readyz (informativo; não reprova a readiness — ver readyz).
const readyTickStaleSeconds = 120.0

func (s *server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	today := time.Now().Format("2006-01-02")

	fmt.Fprintln(w, "# HELP regente_up 1 se o server está no ar.")
	fmt.Fprintln(w, "# TYPE regente_up gauge")
	fmt.Fprintln(w, "regente_up 1")

	// Multi-ambiente: identifica QUAL deployment está respondendo (Dev/Staging/Prod).
	// Dashboards/alertas agrupam por este label; cada ambiente é um deployment
	// separado (workspace/branch + DB + env_label próprios) — ver docs/operacao.md.
	var env string
	_ = s.cfg.DB.QueryRow(`SELECT value FROM settings WHERE key='env_label'`).Scan(&env)
	if env == "" {
		env = "unset"
	}
	fmt.Fprintln(w, "# HELP regente_env_info Ambiente deste deployment (label=env_label).")
	fmt.Fprintln(w, "# TYPE regente_env_info gauge")
	fmt.Fprintf(w, "regente_env_info{env=%q} 1\n", env)

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

		// R7 — liderança deste nó (HA). Prometheus pode alertar em changes() = leader flapping.
		lead := 0
		if s.cfg.Scheduler.IsLeader() {
			lead = 1
		}
		fmt.Fprintln(w, "# HELP regente_is_leader 1 se este nó é o líder do scheduler (HA).")
		fmt.Fprintln(w, "# TYPE regente_is_leader gauge")
		fmt.Fprintf(w, "regente_is_leader %d\n", lead)

		// E4 — profundidade da fila assíncrona de eventos (0 com fila vazia OU
		// desligada; alerte se encostar na capacidade — o excedente degrada pra
		// writes síncronos no hot path).
		depth, _ := s.cfg.Scheduler.EventQueueDepth()
		fmt.Fprintln(w, "# HELP regente_event_queue_depth Eventos de instance aguardando o flush em lote (E4).")
		fmt.Fprintln(w, "# TYPE regente_event_queue_depth gauge")
		fmt.Fprintf(w, "regente_event_queue_depth %d\n", depth)
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

// readyz — R3: readiness. Enquanto /livez pergunta "o processo está vivo?", /readyz
// pergunta "este nó consegue servir respostas CORRETAS?". Gate duro = DB alcançável
// (sem DB nada útil é servido → 503, k8s tira o pod de rotação). Status de líder e
// idade do último tick/daily são REPORTADOS para observabilidade mas NÃO reprovam:
//   - um follower (não-líder) serve a API normalmente;
//   - gatear readiness em tick velho tiraria de rotação o ÚNICO nó saudável, e no modo
//     -scheduler=external o tick vem de cron. Tick parado é alertável via gauge (R7).
func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	type check struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail,omitempty"`
	}
	ready := true
	out := map[string]any{}

	// 1. DB alcançável — gate duro da readiness.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dbCheck := check{OK: true}
	if err := s.cfg.DB.PingContext(ctx); err != nil {
		dbCheck.OK, dbCheck.Detail, ready = false, err.Error(), false
	}
	out["db"] = dbCheck

	// 2. Liderança — informativo (follower é ready).
	if s.cfg.Scheduler != nil {
		if s.cfg.Scheduler.IsLeader() {
			out["leader"] = check{OK: true, Detail: "leader"}
		} else {
			out["leader"] = check{OK: true, Detail: "follower"}
		}
	}

	// 3. Tick do scheduler + último daily — informativo.
	age := -1.0
	stale := false
	if s.cfg.Scheduler != nil {
		if last := s.cfg.Scheduler.LastTick(); !last.IsZero() {
			age = time.Since(last).Seconds()
			stale = age > readyTickStaleSeconds
		}
	}
	out["schedulerLastTickAgeSeconds"] = age
	out["schedulerStale"] = stale
	var lastDaily string
	_ = s.cfg.DB.QueryRow(`SELECT COALESCE(MAX(order_date),'') FROM daily_runs`).Scan(&lastDaily)
	out["lastDaily"] = lastDaily

	out["ready"] = ready
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, out)
}
