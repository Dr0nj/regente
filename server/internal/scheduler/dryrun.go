// Package scheduler — Dry Run ("simular a daily de uma data futura, sem materializar").
//
// Diferencial: prevê uma daily de QUALQUER data (ex.: 25/12) SEM criar instances —
// quem RODA, quem ESPERA (depois de quem) e quem NUNCA dispara (e por quê). Reusa
// IsScheduledOn (a MESMA decisão de agendamento do RunDaily) como fonte única, e
// raciocina sobre o grafo estrutural: deps cujo produtor não é agendado na data, ou
// conditions que nenhum job ativo seta, tornam o job permanentemente bloqueado — em
// CASCATA. Recursos não entram (são contenção de runtime, não "nunca dispara").
package scheduler

import (
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

const dryRunDetailCap = 2000

type DryRunOutcome string

const (
	DryRunRun          DryRunOutcome = "RUN"           // agendado, sem deps pendentes → dispara
	DryRunWait         DryRunOutcome = "WAIT"          // agendado, roda DEPOIS de upstreams (que rodam)
	DryRunBlocked      DryRunOutcome = "BLOCKED"       // agendado mas NUNCA dispara (dep/condition impossível)
	DryRunNotScheduled DryRunOutcome = "NOT_SCHEDULED" // não entra na daily desta data (calendário/frequência)
)

type DryRunJob struct {
	DefID     string        `json:"defId"`
	Label     string        `json:"label,omitempty"`
	Team      string        `json:"team,omitempty"`
	Outcome   DryRunOutcome `json:"outcome"`
	Reason    string        `json:"reason"`
	DependsOn []string      `json:"dependsOn,omitempty"`
}

type DryRunCounts struct {
	Run          int `json:"run"`
	Wait         int `json:"wait"`
	Blocked      int `json:"blocked"`
	NotScheduled int `json:"notScheduled"`
	Total        int `json:"total"`
}

type DryRun struct {
	Date         string       `json:"date"`
	HasCalendars bool         `json:"hasCalendars"`
	Counts       DryRunCounts `json:"counts"`
	Jobs         []DryRunJob  `json:"jobs"`
	Truncated    bool         `json:"truncated"`
}

type dryCls struct {
	def     domain.JobDefinition
	outcome DryRunOutcome
	reason  string
	waitsOn []string
}

// DryRun simula a daily da data dada sem materializar nada (só lê s.defs + calStore).
func (s *Scheduler) DryRun(date string) (DryRun, error) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return DryRun{}, err
	}
	s.mu.Lock()
	defs := make([]domain.JobDefinition, len(s.defs))
	copy(defs, s.defs)
	cal := s.calStore
	s.mu.Unlock()

	dr := DryRun{Date: date, HasCalendars: cal != nil, Jobs: []DryRunJob{}}

	// Conjunto agendado — MESMA decisão do RunDaily (sem calStore = todos habilitados).
	scheduled := map[string]domain.JobDefinition{}
	var enabled []domain.JobDefinition
	for _, d := range defs {
		if !d.Schedule.Enabled {
			continue
		}
		enabled = append(enabled, d)
		if cal != nil && !IsScheduledOn(d, t, cal) {
			continue
		}
		scheduled[d.ID] = d
	}
	dr.Counts.Total = len(enabled)

	// Conditions que ALGUM job agendado produz (ConditionsOutAdd).
	produced := map[string]bool{}
	for _, d := range scheduled {
		for _, c := range d.ConditionsOutAdd {
			produced[c] = true
		}
	}
	condSatisfiable := func(c string) bool {
		if produced[c] {
			return true
		}
		return s.conditions != nil && s.conditions.Has(c, date) // condition permanente já setada
	}

	// 1ª passada: classificação DIRETA dos agendados.
	cls := make(map[string]*dryCls, len(scheduled))
	for id, d := range scheduled {
		c := &dryCls{def: d, outcome: DryRunRun, reason: "eligible — no pending dependencies"}
		for _, u := range d.Upstream {
			if u.Condition == domain.CondAlways {
				continue // always é satisfeita de qualquer jeito → não bloqueia nem espera
			}
			if _, ok := scheduled[u.From]; !ok {
				c.outcome = DryRunBlocked
				c.reason = "depends on '" + u.From + "', which does not run on this date"
				break
			}
			c.waitsOn = append(c.waitsOn, u.From)
		}
		if c.outcome != DryRunBlocked {
			for _, cond := range d.ConditionsIn {
				if !condSatisfiable(cond) {
					c.outcome = DryRunBlocked
					c.reason = "waits for the condition '" + cond + "', which no active job sets on this date"
					break
				}
			}
		}
		if c.outcome != DryRunBlocked && len(c.waitsOn) > 0 {
			c.outcome = DryRunWait
			c.reason = "runs after: " + strings.Join(c.waitsOn, ", ")
		}
		cls[id] = c
	}

	// 2ª passada: propaga BLOCKED em cascata. Se um job ESPERA por outro que NUNCA
	// roda, ele também nunca roda. Sucessores por aresta não-`always` entre agendados.
	succ := map[string][]string{}
	for id, c := range cls {
		for _, up := range c.waitsOn {
			succ[up] = append(succ[up], id)
		}
	}
	queue := []string{}
	for id, c := range cls {
		if c.outcome == DryRunBlocked {
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, y := range succ[cur] {
			if cls[y].outcome != DryRunBlocked {
				cls[y].outcome = DryRunBlocked
				cls[y].reason = "depends on '" + cur + "', which never runs on this date"
				queue = append(queue, y)
			}
		}
	}

	// Monta a saída: agendados classificados + não-agendados.
	add := func(j DryRunJob) {
		switch j.Outcome {
		case DryRunRun:
			dr.Counts.Run++
		case DryRunWait:
			dr.Counts.Wait++
		case DryRunBlocked:
			dr.Counts.Blocked++
		case DryRunNotScheduled:
			dr.Counts.NotScheduled++
		}
		if len(dr.Jobs) < dryRunDetailCap {
			dr.Jobs = append(dr.Jobs, j)
		} else {
			dr.Truncated = true
		}
	}
	for _, d := range enabled {
		if c, ok := cls[d.ID]; ok {
			add(DryRunJob{DefID: d.ID, Label: d.Label, Team: d.Team, Outcome: c.outcome, Reason: c.reason, DependsOn: c.waitsOn})
		} else {
			add(DryRunJob{DefID: d.ID, Label: d.Label, Team: d.Team, Outcome: DryRunNotScheduled,
				Reason: "outside the calendar/frequency on this date"})
		}
	}
	return dr, nil
}
