// Package scheduler — Diff de Daily ("o que mudou entre duas diárias?").
//
// Diferencial Git-native: cada instance carrega definition_commit_sha + o snapshot
// CONGELADO da def no momento da ordem. Então comparar duas dailies (order_date) é
// EXATO e barato — sem reprocessar Git, sem reconstruir nada. Reporta:
//   - ADICIONADOS  defs que entraram na daily B (não estavam em A)
//   - REMOVIDOS    defs que saíram (estavam em A, não em B)
//   - ALTERADOS    defs em ambas cujo snapshot mudou — com diff POR-CAMPO
//     (schedule, deps, recursos, conditions, params, SLA, …)
//
// Fast-path: numa daily todas as instances compartilham UM commit (RunDaily
// carimba o mesmo sha). Se commitA==commitB, nenhum def comum pode ter mudado
// (mesmo commit ⟹ mesmo conteúdo) → pula a comparação de snapshots inteira.
package scheduler

import (
	"encoding/json"
	"strconv"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// diffDetailCap — teto de itens detalhados por categoria na resposta (os CONTADORES
// são sempre exatos; só as listas são capadas, sinalizando Truncated).
const diffDetailCap = 1000

type DefRef struct {
	DefID string `json:"defId"`
	Label string `json:"label,omitempty"`
	Team  string `json:"team,omitempty"`
}

type FieldChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type DefChange struct {
	DefID   string        `json:"defId"`
	Label   string        `json:"label,omitempty"`
	Team    string        `json:"team,omitempty"`
	Changes []FieldChange `json:"changes"`
}

type DiffCounts struct {
	TotalA    int `json:"totalA"`
	TotalB    int `json:"totalB"`
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

type DailyDiff struct {
	DateA      string      `json:"dateA"`
	DateB      string      `json:"dateB"`
	Folder     string      `json:"folder,omitempty"`
	CommitA    string      `json:"commitA,omitempty"`
	CommitB    string      `json:"commitB,omitempty"`
	SameCommit bool        `json:"sameCommit"`
	Counts     DiffCounts  `json:"counts"`
	Added      []DefRef    `json:"added"`
	Removed    []DefRef    `json:"removed"`
	Changed    []DefChange `json:"changed"`
	Truncated  bool        `json:"truncated"`
}

// PrevDailyDate — o order_date materializado mais recente ANTES de `before`
// (a "diária anterior"). Vazio se não houver. Usado como default do diff
// ("hoje vs a daily anterior"), robusto a fins de semana/dias pulados.
func (s *Scheduler) PrevDailyDate(before string) string {
	var d string
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(order_date),'') FROM instances WHERE order_date < ?`, before).Scan(&d)
	return d
}

// DiffDaily compara a daily de dateA com a de dateB (opcionalmente escopada a uma
// folder/team). Lê só o estado durável (snapshots congelados) — nenhuma operação Git.
func (s *Scheduler) DiffDaily(dateA, dateB, folder string) (DailyDiff, error) {
	a, commitA, err := s.loadDailyDefs(dateA, folder)
	if err != nil {
		return DailyDiff{}, err
	}
	b, commitB, err := s.loadDailyDefs(dateB, folder)
	if err != nil {
		return DailyDiff{}, err
	}

	diff := DailyDiff{
		DateA: dateA, DateB: dateB, Folder: folder,
		CommitA: commitA, CommitB: commitB,
		SameCommit: commitA != "" && commitA == commitB,
		Added:      []DefRef{}, Removed: []DefRef{}, Changed: []DefChange{},
	}
	diff.Counts.TotalA = len(a)
	diff.Counts.TotalB = len(b)

	// REMOVIDOS: em A e não em B.
	for id, snap := range a {
		if _, ok := b[id]; ok {
			continue
		}
		diff.Counts.Removed++
		if len(diff.Removed) < diffDetailCap {
			diff.Removed = append(diff.Removed, defRefFromSnap(id, snap))
		} else {
			diff.Truncated = true
		}
	}

	// ADICIONADOS + ALTERADOS: percorre B.
	for id, snapB := range b {
		snapA, ok := a[id]
		if !ok {
			diff.Counts.Added++
			if len(diff.Added) < diffDetailCap {
				diff.Added = append(diff.Added, defRefFromSnap(id, snapB))
			} else {
				diff.Truncated = true
			}
			continue
		}
		// Comum. Fast-path: mesmo commit ⟹ idêntico; ou snapshot byte-igual.
		if diff.SameCommit || snapA == snapB {
			diff.Counts.Unchanged++
			continue
		}
		// Snapshots diferem → diff por-campo (só aqui paga o parse).
		var da, db domain.JobDefinition
		_ = json.Unmarshal([]byte(snapA), &da)
		_ = json.Unmarshal([]byte(snapB), &db)
		changes := diffDefs(da, db)
		if len(changes) == 0 {
			// Strings diferem mas os campos normalizados são iguais (ruído) → unchanged.
			diff.Counts.Unchanged++
			continue
		}
		diff.Counts.Changed++
		if len(diff.Changed) < diffDetailCap {
			diff.Changed = append(diff.Changed, DefChange{DefID: id, Label: db.Label, Team: db.Team, Changes: changes})
		} else {
			diff.Truncated = true
		}
	}

	return diff, nil
}

// loadDailyDefs devolve defID → snapshot (1 por def, dedupado) e o commit da daily.
func (s *Scheduler) loadDailyDefs(date, folder string) (map[string]string, string, error) {
	q := `SELECT definition_id, COALESCE(definition_snapshot,''), COALESCE(definition_commit_sha,'')
	      FROM instances WHERE order_date=?`
	args := []any{date}
	if folder != "" {
		q += ` AND team=?`
		args = append(args, folder)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := map[string]string{}
	commit := ""
	for rows.Next() {
		var id, snap, sha string
		if rows.Scan(&id, &snap, &sha) != nil {
			continue
		}
		if _, ok := out[id]; !ok {
			out[id] = snap
		}
		if commit == "" && sha != "" {
			commit = sha
		}
	}
	return out, commit, nil
}

func defRefFromSnap(id, snap string) DefRef {
	r := DefRef{DefID: id}
	if snap != "" {
		var d domain.JobDefinition
		if json.Unmarshal([]byte(snap), &d) == nil {
			r.Label, r.Team = d.Label, d.Team
		}
	}
	return r
}

// diffDefs — diff POR-CAMPO entre duas defs (já desserializadas). Campos
// estruturados são comparados via JSON compacto (determinístico no Go: chaves de
// map saem ordenadas). Adicionar campo novo aqui = mais uma linha.
func diffDefs(a, b domain.JobDefinition) []FieldChange {
	j := func(v any) string {
		bs, _ := json.Marshal(v)
		s := string(bs)
		if s == "null" {
			return ""
		}
		return s
	}
	fields := []struct{ name, av, bv string }{
		{"label", a.Label, b.Label},
		{"jobType", a.JobType, b.JobType},
		{"team", a.Team, b.Team},
		{"schedule", j(a.Schedule), j(b.Schedule)},
		{"upstream", j(a.Upstream), j(b.Upstream)},
		{"resources", j(a.Resources), j(b.Resources)},
		{"conditionsIn", j(a.ConditionsIn), j(b.ConditionsIn)},
		{"conditionsOut", j([]any{a.ConditionsOutAdd, a.ConditionsOutRemove}), j([]any{b.ConditionsOutAdd, b.ConditionsOutRemove})},
		{"params", j(a.Params), j(b.Params)},
		{"calendars", j(a.Calendars), j(b.Calendars)},
		{"sla", j(a.SLA), j(b.SLA)},
		{"variables", j(a.Variables), j(b.Variables)},
		{"retries", strconv.Itoa(a.Retries), strconv.Itoa(b.Retries)},
		{"timeout", strconv.Itoa(a.Timeout), strconv.Itoa(b.Timeout)},
		{"environment", a.Environment, b.Environment},
	}
	var out []FieldChange
	for _, f := range fields {
		if f.av != f.bv {
			out = append(out, FieldChange{Field: f.name, From: f.av, To: f.bv})
		}
	}
	return out
}
