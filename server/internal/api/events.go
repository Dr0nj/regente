// Package api — Event log (CQRS-lite): read-model consolidado sobre a trilha de
// eventos por instância.
//
// O write-side já existe: cada transição grava em `instance_events` (ordered,
// started, submitted, finished, retry, cyclic, set-ok, confirmed, held, …). Este
// é o QUERY-side: um feed CROSS-instance do dia, filtrável por kind/actor/folder/
// instance, paginado por cursor, com RBAC por conjunto de folders — sem tocar o
// caminho de escrita. É o substrato do "o que aconteceu hoje?" (auditoria,
// timeline operacional, alimentação de dashboards).
package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

type eventLogRow struct {
	ID         int64     `json:"id"`
	InstanceID string    `json:"instanceId"`
	Team       string    `json:"team,omitempty"`
	OrderDate  string    `json:"orderDate"`
	TS         time.Time `json:"ts"`
	Kind       string    `json:"kind"`
	Actor      string    `json:"actor,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// listEventLog — GET /api/events. Feed cross-instance do dia.
//
//	date     YYYY-MM-DD (default hoje)
//	kind     lista separada por vírgula (ex.: finished,retry)
//	actor    LIKE (ex.: "operator", "agent")
//	folder   team exato
//	instance instance_id exato
//	limit    (default 200, teto 5000)
//	cursor   id da última linha da página anterior (keyset desc por id)
func (s *server) listEventLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	date := q.Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	clauses := []string{"i.order_date = ?"}
	args := []any{date}

	if kinds := q.Get("kind"); kinds != "" {
		var ks []string
		for _, k := range strings.Split(kinds, ",") {
			if k = strings.TrimSpace(k); k != "" {
				ks = append(ks, k)
			}
		}
		if len(ks) > 0 {
			clauses = append(clauses, "e.kind IN ("+placeholders(len(ks))+")")
			for _, k := range ks {
				args = append(args, k)
			}
		}
	}
	if actor := strings.TrimSpace(q.Get("actor")); actor != "" {
		clauses = append(clauses, "e.actor LIKE ?")
		args = append(args, "%"+actor+"%")
	}
	if folder := q.Get("folder"); folder != "" {
		clauses = append(clauses, "i.team = ?")
		args = append(args, folder)
	}
	if inst := q.Get("instance"); inst != "" {
		clauses = append(clauses, "e.instance_id = ?")
		args = append(args, inst)
	}

	// RBAC por CONJUNTO (mesma régua de listInstances): não-admin só vê folders legíveis.
	allowed, restrict := s.allowedTeams(r, date)
	if restrict {
		if len(allowed) == 0 {
			clauses = append(clauses, "1=0")
		} else {
			clauses = append(clauses, "i.team IN ("+placeholders(len(allowed))+")")
			for _, t := range allowed {
				args = append(args, t)
			}
		}
	}

	// Cursor keyset por id decrescente (id é monotônico crescente na inserção).
	if cur := q.Get("cursor"); cur != "" {
		if n, err := strconv.ParseInt(cur, 10, 64); err == nil {
			clauses = append(clauses, "e.id < ?")
			args = append(args, n)
		}
	}

	limit := parseLimit(r, 200)
	if limit == 0 {
		limit = 200
	}

	sqlStr := `SELECT e.id, e.instance_id, COALESCE(i.team,''), i.order_date, e.ts, e.kind, COALESCE(e.actor,''), COALESCE(e.message,'')
		FROM instance_events e JOIN instances i ON e.instance_id = i.id
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY e.id DESC LIMIT ?`
	args = append(args, limit+1) // +1 para saber se há próxima página

	rows, err := s.cfg.DB.Query(sqlStr, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []eventLogRow{}
	for rows.Next() {
		var ev eventLogRow
		if err := rows.Scan(&ev.ID, &ev.InstanceID, &ev.Team, &ev.OrderDate, &ev.TS, &ev.Kind, &ev.Actor, &ev.Message); err != nil {
			continue
		}
		items = append(items, ev)
	}
	if !rowsOK(w, rows) {
		return
	}

	next := ""
	if len(items) > limit {
		next = strconv.FormatInt(items[limit-1].ID, 10)
		items = items[:limit]
	}
	writeJSON(w, 200, map[string]any{"date": date, "items": items, "nextCursor": next})
}
