// OL-2 — API do sysout da EXECUÇÃO (instance_output), separada do jornal de
// AGENDAMENTO (instance_events / /events). Ver docs/roadmap.md §Output × Logs.
package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

// includesOutput — por default os leitores de instance_events (aba Logs, feed do
// dia /api/events, export SIEM) ESCONDEM o kind=output LEGADO — sysout que foi
// gravado como evento antes da OL-1 movê-lo pra instance_output. `?include=output`
// (ou `?include=all`) mantém esse histórico legível: migração não destrutiva,
// sem reescrever dado velho.
func includesOutput(r *http.Request) bool {
	inc := strings.TrimSpace(r.URL.Query().Get("include"))
	return inc == "output" || inc == "all"
}

// getInstanceOutput — GET /api/instances/{id}/output?attempt=N. O sysout da
// EXECUÇÃO por TENTATIVA: RUNNING serve o concat dos chunks (live-tail);
// terminada, o consolidado. Sem `attempt`, devolve a tentativa CORRENTE.
// Resposta: {attempts, attempt, text, complete, exitCode?}.
func (s *server) getInstanceOutput(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var status, consolidated string
	var exit sql.NullInt64
	var curAttempt int
	err := s.cfg.DB.QueryRow(
		`SELECT status, exit_code, COALESCE(attempts,1), COALESCE(output,'') FROM instances WHERE id=?`, id,
	).Scan(&status, &exit, &curAttempt, &consolidated)
	if err == sql.ErrNoRows {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if curAttempt < 1 {
		curAttempt = 1
	}
	attempt := curAttempt
	if a := strings.TrimSpace(r.URL.Query().Get("attempt")); a != "" {
		if n, perr := strconv.Atoi(a); perr == nil && n >= 1 {
			attempt = n
		}
	}

	// Concat dos chunks da tentativa (ordem = seq monotônico do id). Os chunks são
	// gravados verbatim (com quebras de linha), então o concat reproduz o stream.
	var sb strings.Builder
	rows, err := s.cfg.DB.Query(
		`SELECT chunk FROM instance_output WHERE instance_id=? AND attempt=? ORDER BY id`, id, attempt,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var chunk string
		if err := rows.Scan(&chunk); err != nil {
			continue
		}
		sb.WriteString(chunk)
	}
	if !rowsOK(w, rows) {
		return
	}
	text := sb.String()

	terminal := status == string(domain.StatusOK) ||
		status == string(domain.StatusNotOK) ||
		status == string(domain.StatusCancelled)

	// Compat/fallback: instances ANTIGAS (pré-OL) não têm chunks em
	// instance_output; se a tentativa pedida é a final e a run terminou, serve o
	// consolidado gravado em instances.output — a aba Output segue funcionando
	// para runs históricas sem reescrever nada.
	if text == "" && terminal && attempt == curAttempt {
		text = consolidated
	}

	// complete = a tentativa não recebe mais chunks: ou a instance terminou, ou é
	// uma tentativa ANTERIOR (uma retry já a superou).
	complete := terminal || attempt < curAttempt

	resp := map[string]any{
		"attempts": curAttempt,
		"attempt":  attempt,
		"text":     text,
		"complete": complete,
	}
	// exitCode só faz sentido na tentativa final terminada.
	if terminal && attempt == curAttempt && exit.Valid {
		resp["exitCode"] = exit.Int64
	}
	writeJSON(w, 200, resp)
}
