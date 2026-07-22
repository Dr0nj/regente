// E2 — Auditoria enterprise: export unificado em JSONL.
//
// GET /api/audit/export?from=&to=&after_id=&limit=&format=jsonl  (admin-only)
//
// Devolve instance_events (source "instance") e audit_events (source "audit")
// unificados, uma linha JSON por evento, em streaming. Paginação por cursor
// ESTÁVEL: as fontes saem em sequência (todas as instance_events, depois as
// audit_events), cada uma ordenada por id — cada linha carrega seu próprio
// `cursor` ("i:<id>" | "a:<id>"); se a resposta veio com `limit` linhas,
// chame de novo com after_id = cursor da última linha (tail -1 | jq -r .cursor).
//
//   - from/to: "YYYY-MM-DD" (meia-noite UTC) ou RFC3339; from inclusivo, to
//     exclusivo. Filtram pelo ts do evento.
//   - limit: default e teto 100_000 linhas por chamada.
//   - format: só "jsonl" (default).
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
)

// exportMaxLines — teto de linhas por chamada (spec E2).
const exportMaxLines = 100_000

// exportLine — o registro unificado {ts, kind, actor, instanceId?, detail}.
type exportLine struct {
	Cursor     string `json:"cursor"`
	Source     string `json:"source"` // "instance" | "audit"
	TS         string `json:"ts"`
	Kind       string `json:"kind"`
	Actor      string `json:"actor,omitempty"`
	InstanceID string `json:"instanceId,omitempty"`
	Action     string `json:"action,omitempty"`
	Target     string `json:"target,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	IP         string `json:"ip,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// parseExportTime aceita data pura (YYYY-MM-DD, meia-noite UTC) ou RFC3339.
func parseExportTime(v string) (time.Time, bool) {
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}

// tsArg — parâmetro de comparação temporal dialect-safe: o SQLite armazena
// DATETIME como texto 'YYYY-MM-DD HH:MM:SS' (UTC), então uma string no MESMO
// formato compara certo; o Postgres (TIMESTAMPTZ) recebe time.Time nativo.
func (s *server) tsArg(t time.Time) any {
	if s.cfg.DB.Dialect() == db.Postgres {
		return t.UTC()
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

// auditExport — GET /api/audit/export.
func (s *server) auditExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	q := r.URL.Query()
	if f := q.Get("format"); f != "" && f != "jsonl" {
		http.Error(w, "format não suportado (use jsonl)", http.StatusBadRequest)
		return
	}
	limit := exportMaxLines
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "limit inválido", http.StatusBadRequest)
			return
		}
		if n < limit {
			limit = n
		}
	}
	var from, to time.Time
	if v := q.Get("from"); v != "" {
		t, ok := parseExportTime(v)
		if !ok {
			http.Error(w, "from inválido (use YYYY-MM-DD ou RFC3339)", http.StatusBadRequest)
			return
		}
		from = t
	}
	if v := q.Get("to"); v != "" {
		t, ok := parseExportTime(v)
		if !ok {
			http.Error(w, "to inválido (use YYYY-MM-DD ou RFC3339)", http.StatusBadRequest)
			return
		}
		to = t
	}
	// Cursor: ""=começo · "i:<id>"=retoma instance_events · "a:<id>"=instance_events
	// já esgotadas, retoma audit_events.
	afterSource, afterID := "", int64(0)
	if cur := q.Get("after_id"); cur != "" {
		parts := strings.SplitN(cur, ":", 2)
		id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		if err != nil || (len(parts) == 2 && parts[0] != "i" && parts[0] != "a") {
			http.Error(w, "after_id inválido (use o cursor da última linha, ex. i:123)", http.StatusBadRequest)
			return
		}
		afterID = id
		if len(parts) == 2 {
			afterSource = parts[0]
		} else {
			afterSource = "i" // compat: cursor sem prefixo = instance_events
		}
	}

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	enc := json.NewEncoder(w)
	written := 0

	// timeWhere monta o filtro temporal comum às duas fontes.
	timeWhere := func(clauses []string, args []any) ([]string, []any) {
		if !from.IsZero() {
			clauses = append(clauses, "ts >= ?")
			args = append(args, s.tsArg(from))
		}
		if !to.IsZero() {
			clauses = append(clauses, "ts < ?")
			args = append(args, s.tsArg(to))
		}
		return clauses, args
	}

	// 1ª fonte: instance_events (pulada se o cursor já está em "a").
	if afterSource != "a" {
		clauses := []string{"id > ?"}
		args := []any{afterID}
		clauses, args = timeWhere(clauses, args)
		// OL-2 — o export SIEM é AUDITORIA; sysout legado (kind=output gravado
		// antes da OL-1) infla o export sem ser auditoria. Excluído por default;
		// ?include=output mantém o histórico legível.
		if !includesOutput(r) {
			clauses = append(clauses, "kind != 'output'")
		}
		rows, err := s.cfg.DB.Query(
			`SELECT id, ts, kind, COALESCE(actor,''), instance_id, COALESCE(message,'')
			 FROM instance_events WHERE `+strings.Join(clauses, " AND ")+` ORDER BY id LIMIT ?`,
			append(args, limit)...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			var id int64
			var ts time.Time
			var line exportLine
			if rows.Scan(&id, &ts, &line.Kind, &line.Actor, &line.InstanceID, &line.Detail) != nil {
				continue
			}
			line.Cursor = "i:" + strconv.FormatInt(id, 10)
			line.Source = "instance"
			line.TS = ts.UTC().Format(time.RFC3339)
			_ = enc.Encode(line)
			written++
		}
		// Stream truncado é SEGURO (cada linha carrega o cursor; o cliente
		// retoma dali) — mas o motivo precisa aparecer no log.
		if err := rows.Err(); err != nil {
			log.Printf("[export] instance_events: iteração incompleta (cliente retoma pelo cursor): %v", err)
		}
		rows.Close()
		if written >= limit {
			return
		}
		afterID = 0 // audit_events começa do zero quando a 1ª fonte esgotou nesta chamada
	}

	// 2ª fonte: audit_events, com o orçamento de linhas restante.
	clauses := []string{"id > ?"}
	args := []any{afterID}
	clauses, args = timeWhere(clauses, args)
	rows, err := s.cfg.DB.Query(
		`SELECT id, ts, kind, actor, action, target, outcome, ip, detail
		 FROM audit_events WHERE `+strings.Join(clauses, " AND ")+` ORDER BY id LIMIT ?`,
		append(args, limit-written)...)
	if err != nil {
		if written == 0 {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var ts time.Time
		var line exportLine
		if rows.Scan(&id, &ts, &line.Kind, &line.Actor, &line.Action, &line.Target,
			&line.Outcome, &line.IP, &line.Detail) != nil {
			continue
		}
		line.Cursor = "a:" + strconv.FormatInt(id, 10)
		line.Source = "audit"
		line.TS = ts.UTC().Format(time.RFC3339)
		_ = enc.Encode(line)
		written++
	}
	if err := rows.Err(); err != nil {
		log.Printf("[export] audit_events: iteração incompleta (cliente retoma pelo cursor): %v", err)
	}
}
