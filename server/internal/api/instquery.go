// Package api — D-5 Query estruturada / busca rica sobre o estado.
//
// POST /api/instances/query — filtro COMPOSTO e TIPADO além do que a query
// string de /api/instances cobre: range de datas, listas IN (folders/statuses),
// flags (forced/carried/late) e agregação (groupBy) — bounded, NÃO é
// SQL-sobre-HTTP: todo filtro é um campo tipado com placeholder; parse ESTRITO
// (campo desconhecido = 400, mesma filosofia do modo CODE).
//
// Transporte (decisão registrada no roadmap em 2026-06-24): POST é o baseline
// universal; o MESMO handler aceita o método HTTP QUERY (draft IETF httpbis —
// SAFE+idempotente como GET, body como POST) como progressive enhancement para
// CLI/integrações/MCP. Em Go isso é só uma rota extra (chi roteia método
// custom), zero dependência.
//
// Consumidores: dashboards, integrações e a camada agent-native (tool MCP de
// query estruturada). Read-only; RBAC pelo mesmo conjunto de folders legíveis.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
)

// structuredQuery — o corpo tipado. Tudo opcional; default = hoje, sem filtro.
type structuredQuery struct {
	Date        string   `json:"date"`        // dia exato (YYYY-MM-DD)
	From        string   `json:"from"`        // ...ou range inclusivo from..to
	To          string   `json:"to"`          //
	Folders     []string `json:"folders"`     // IN
	Statuses    []string `json:"statuses"`    // IN
	Search      string   `json:"search"`      // LIKE id/definition_id
	DefIDPrefix string   `json:"defIdPrefix"` // definition_id LIKE 'prefix%'
	Forced      *bool    `json:"forced"`      // só forçados / só não-forçados
	Carried     *bool    `json:"carried"`     // só carry-over de outra diária
	Late        *bool    `json:"late"`        // WAITING com scheduled_at no passado (atrasados)
	GroupBy     string   `json:"groupBy"`     // "" | "status" | "folder" | "definition"
	Limit       int      `json:"limit"`       // teto 5000 (default 500)
	Cursor      string   `json:"cursor"`      // keyset (scheduled_at,id) — só sem groupBy
}

// where monta o filtro parametrizado. `allowed`/`restrict` = RBAC por conjunto.
func (q structuredQuery) where(allowed []string, restrict bool, now time.Time) (string, []any) {
	clauses := []string{}
	args := []any{}
	switch {
	case q.From != "" && q.To != "":
		clauses, args = append(clauses, "order_date BETWEEN ? AND ?"), append(args, q.From, q.To)
	case q.From != "":
		clauses, args = append(clauses, "order_date >= ?"), append(args, q.From)
	case q.To != "":
		clauses, args = append(clauses, "order_date <= ?"), append(args, q.To)
	default:
		d := q.Date
		if d == "" {
			d = now.Format("2006-01-02")
		}
		clauses, args = append(clauses, "order_date = ?"), append(args, d)
	}
	if len(q.Folders) > 0 {
		clauses = append(clauses, "team IN ("+placeholders(len(q.Folders))+")")
		for _, f := range q.Folders {
			args = append(args, f)
		}
	}
	if len(q.Statuses) > 0 {
		clauses = append(clauses, "status IN ("+placeholders(len(q.Statuses))+")")
		for _, st := range q.Statuses {
			args = append(args, st)
		}
	}
	if q.Search != "" {
		like := "%" + q.Search + "%"
		clauses, args = append(clauses, "(id LIKE ? OR definition_id LIKE ?)"), append(args, like, like)
	}
	if q.DefIDPrefix != "" {
		clauses, args = append(clauses, "definition_id LIKE ?"), append(args, q.DefIDPrefix+"%")
	}
	if q.Forced != nil {
		if *q.Forced {
			clauses = append(clauses, "COALESCE(forced,0)=1")
		} else {
			clauses = append(clauses, "COALESCE(forced,0)=0")
		}
	}
	if q.Carried != nil {
		if *q.Carried {
			clauses = append(clauses, "COALESCE(carried_from,'') <> ''")
		} else {
			clauses = append(clauses, "COALESCE(carried_from,'') = ''")
		}
	}
	if q.Late != nil && *q.Late {
		clauses, args = append(clauses, "status='WAITING' AND scheduled_at < ?"), append(args, now)
	}
	if restrict {
		if len(allowed) == 0 {
			clauses = append(clauses, "1=0")
		} else {
			clauses = append(clauses, "team IN ("+placeholders(len(allowed))+")")
			for _, t := range allowed {
				args = append(args, t)
			}
		}
	}
	where := "1=1"
	if len(clauses) > 0 {
		where = clauses[0]
		for _, c := range clauses[1:] {
			where += " AND " + c
		}
	}
	return where, args
}

// queryInstances — handler de POST e QUERY /api/instances/query.
func (s *server) queryInstances(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // tipado de verdade: campo desconhecido = erro, não silêncio
	var q structuredQuery
	if err := dec.Decode(&q); err != nil {
		http.Error(w, "invalid query: "+err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	allowed, restrict := s.allowedTeamsRange(r, q)
	where, args := q.where(allowed, restrict, now)

	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	// Agregação: contadores no banco, nenhuma linha materializada.
	if q.GroupBy != "" {
		col := ""
		switch q.GroupBy {
		case "status":
			col = "status"
		case "folder":
			col = "team"
		case "definition":
			col = "definition_id"
		default:
			http.Error(w, "groupBy must be status|folder|definition", http.StatusBadRequest)
			return
		}
		rows, err := s.cfg.DB.Query(
			`SELECT `+col+`, COUNT(*) AS n FROM instances WHERE `+where+
				` GROUP BY `+col+` ORDER BY n DESC LIMIT ?`, append(args, limit)...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		type group struct {
			Key   string `json:"key"`
			Count int    `json:"count"`
		}
		groups := []group{}
		total := 0
		for rows.Next() {
			var g group
			if rows.Scan(&g.Key, &g.Count) == nil {
				groups = append(groups, g)
				total += g.Count
			}
		}
		if !rowsOK(w, rows) {
			return
		}
		writeJSON(w, 200, map[string]any{"groupBy": q.GroupBy, "groups": groups, "total": total})
		return
	}

	// Lista paginada por keyset (scheduled_at,id) — o mesmo cursor do /instances/page.
	if q.Cursor != "" {
		where += ` AND (scheduled_at > (SELECT scheduled_at FROM instances WHERE id=?)
		           OR (scheduled_at = (SELECT scheduled_at FROM instances WHERE id=?) AND id > ?))`
		args = append(args, q.Cursor, q.Cursor, q.Cursor)
	}
	rows, err := s.cfg.DB.Query(
		`SELECT `+instanceCols+` FROM instances WHERE `+where+` ORDER BY scheduled_at, id LIMIT ?`,
		append(args, limit+1)...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items, err := scanInstances(rows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next := ""
	if len(items) > limit {
		next = items[limit-1].ID
		items = items[:limit]
	}
	writeJSON(w, 200, map[string]any{"items": items, "nextCursor": next})
}

// allowedTeamsRange — RBAC para queries que podem cruzar VÁRIOS order_dates:
// o universo de folders candidatas vem do range, não de um dia único.
func (s *server) allowedTeamsRange(r *http.Request, q structuredQuery) ([]string, bool) {
	u, ok := auth.FromContext(r.Context())
	if !ok || u == nil || u.Role == auth.RoleAdmin {
		return nil, false
	}
	sqlStr, args := "SELECT DISTINCT team FROM instances WHERE order_date=?", []any{q.Date}
	switch {
	case q.From != "" && q.To != "":
		sqlStr, args = "SELECT DISTINCT team FROM instances WHERE order_date BETWEEN ? AND ?", []any{q.From, q.To}
	case q.From != "":
		sqlStr, args = "SELECT DISTINCT team FROM instances WHERE order_date >= ?", []any{q.From}
	case q.To != "":
		sqlStr, args = "SELECT DISTINCT team FROM instances WHERE order_date <= ?", []any{q.To}
	case q.Date == "":
		sqlStr, args = "SELECT DISTINCT team FROM instances WHERE order_date=?", []any{time.Now().Format("2006-01-02")}
	}
	// Fail-closed como o allowedTeams de dia único: parcial só ESTREITA a visão.
	var distinct []string
	if rows, err := s.cfg.DB.Query(sqlStr, args...); err == nil {
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil && t != "" {
				distinct = append(distinct, t)
			}
		}
		if err := rows.Err(); err != nil {
			log.Printf("[api] allowedTeamsRange: incomplete iteration (RBAC fail-closed): %v", err)
		}
		rows.Close()
	} else {
		log.Printf("[api] allowedTeamsRange: query (RBAC fail-closed): %v", err)
	}
	readable, _ := auth.FilterReadableFolders(s.cfg.DB, u, distinct)
	return readable, true
}
