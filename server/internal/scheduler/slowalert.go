// Package scheduler — Slow Execution pela MÉDIA histórica (2026-07-17).
//
// A rule-slow deixou de ser um teto fixo (30s): um job é "lento" quando
// ULTRAPASSA a própria média histórica de execuções OK + a folga configurada
// (percentOver, default 50%). Primeira execução (sem histórico) nunca alerta.
// O disparo acontece DURANTE a execução (varredura das RUNNING a cada tick),
// não só no término — "job de 10min alerta ao cruzar 15min" (report do
// usuário); o término ainda avalia, deduplicado por run (slowFired).
package scheduler

import (
	"encoding/json"
	"log"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

const slowAvgWindow = 30 // mesmas ~30 runs do PerfForecast (D-4)

// slowAvgTTL — validade do cache da média por definition. A média muda devagar
// (só quando uma run OK termina); 1min mantém a varredura do tick barata.
const slowAvgTTL = time.Minute

type slowAvgEntry struct {
	avgMs int64
	runs  int
	at    time.Time
}

// avgOKDurationMs — média das últimas execuções OK da definition, excluindo
// excludeID (a run corrente não pode puxar a própria régua). runs=0 ⇒ primeira
// execução: a regra de lentidão não se aplica.
func (s *Scheduler) avgOKDurationMs(defID, excludeID string) (int64, int) {
	rows, err := s.db.Query(
		`SELECT started_at, finished_at FROM instances
		 WHERE definition_id=? AND id<>? AND status='OK'
		   AND started_at IS NOT NULL AND finished_at IS NOT NULL
		 ORDER BY finished_at DESC LIMIT ?`, defID, excludeID, slowAvgWindow)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()
	var total int64
	var n int
	for rows.Next() {
		var st, fi time.Time
		if rows.Scan(&st, &fi) == nil {
			if ms := fi.Sub(st).Milliseconds(); ms >= 0 {
				total += ms
				n++
			}
		}
	}
	if err := rows.Err(); err != nil {
		// Média sobre histórico incompleto puxaria a régua pra baixo → falso
		// "slow"; sem régua a regra simplesmente não se aplica neste ciclo.
		return 0, 0
	}
	if n == 0 {
		return 0, 0
	}
	return total / int64(n), n
}

// cachedAvg — avgOKDurationMs com TTL por definition. Só para a varredura de
// RUNNING do tick; o caminho terminal (buildAlertContext) consulta direto,
// porque precisa excluir a própria run recém-gravada.
func (s *Scheduler) cachedAvg(defID string) (int64, int) {
	s.slowMu.Lock()
	e, ok := s.slowAvg[defID]
	s.slowMu.Unlock()
	if ok && time.Since(e.at) < slowAvgTTL {
		return e.avgMs, e.runs
	}
	avg, runs := s.avgOKDurationMs(defID, "")
	s.slowMu.Lock()
	s.slowAvg[defID] = slowAvgEntry{avgMs: avg, runs: runs, at: time.Now()}
	s.slowMu.Unlock()
	return avg, runs
}

// evaluateSlowRunning — varre as instances RUNNING e dispara o Slow Execution
// de quem já estourou média + folga. Chamada a cada tick; deduplicada por
// instance (slowFired) — cada run alerta no máximo uma vez.
func (s *Scheduler) evaluateSlowRunning(now time.Time) {
	if s.alerts == nil {
		return
	}
	rules := s.alerts.SlowRules()
	if len(rules) == 0 {
		return
	}
	// Menor folga entre as regras — pré-filtro barato antes de montar contexto.
	minPct := -1.0
	for _, r := range rules {
		var cond alertCondition
		if json.Unmarshal([]byte(r.ConditionJSON), &cond) == nil {
			pct := cond.PercentOver
			if pct <= 0 {
				pct = 50
			}
			if minPct < 0 || pct < minPct {
				minPct = pct
			}
		}
	}
	if minPct < 0 {
		minPct = 50
	}

	rows, err := s.db.Query(
		`SELECT id, definition_id, started_at FROM instances
		 WHERE status='RUNNING' AND started_at IS NOT NULL`)
	if err != nil {
		return
	}
	type cand struct {
		id, defID string
		elapsedMs int64
		avgMs     int64
		runs      int
	}
	var cands []cand
	alive := map[string]bool{}
	for rows.Next() {
		var id, defID string
		var started time.Time
		if rows.Scan(&id, &defID, &started) != nil {
			continue
		}
		alive[id] = true
		s.slowMu.Lock()
		fired := s.slowFired[id]
		s.slowMu.Unlock()
		if fired {
			continue
		}
		avg, runs := s.cachedAvg(defID)
		if runs == 0 || avg <= 0 {
			continue // primeira execução — ainda não existe régua
		}
		elapsed := now.Sub(started).Milliseconds()
		if float64(elapsed) <= float64(avg)*(1+minPct/100) {
			continue
		}
		cands = append(cands, cand{id, defID, elapsed, avg, runs})
	}
	errIter := rows.Err()
	rows.Close()
	if errIter != nil {
		// `alive` incompleto podaria slowFired de runs AINDA vivas → alerta de
		// lentidão re-disparado em duplicata. Aborta o ciclo; o próximo tick refaz.
		log.Printf("[alerts] slow scan: iteração incompleta (ciclo pulado): %v", errIter)
		return
	}

	// Poda do ledger: runs que saíram de RUNNING (terminadas, Set OK, cancel,
	// delete) liberam a entrada — o caminho terminal também consome a sua.
	s.slowMu.Lock()
	for id := range s.slowFired {
		if !alive[id] {
			delete(s.slowFired, id)
		}
	}
	s.slowMu.Unlock()

	for _, c := range cands {
		ctx := s.buildAlertContext(c.id, domain.StatusRunning)
		ctx.Running = true
		ctx.DurationMs = c.elapsedMs
		ctx.AvgDurationMs = c.avgMs
		ctx.HistoryRuns = c.runs
		ctx.SlowFired = false // a régua do dedupe aqui é o próprio slowFired
		if s.alerts.FireSlowRules(rules, ctx) {
			s.slowMu.Lock()
			s.slowFired[c.id] = true
			s.slowMu.Unlock()
		}
	}
}
