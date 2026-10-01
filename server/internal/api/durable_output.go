package api

import (
	"database/sql"
	"errors"
	"github.com/Dr0nj/regente-server/internal/execution"
	"net/http"
	"strconv"
	"strings"
)

func (s *server) durableOutput(w http.ResponseWriter, r *http.Request, id string) bool {
	if s.attempts == nil {
		return false
	}
	o, err := s.attempts.RuntimeOrder(id)
	if errors.Is(err, execution.ErrNotFound) {
		return false
	}
	if err != nil {
		executionError(w, err)
		return true
	}
	current := o.Attempt
	attempt := current
	if query := r.URL.Query().Get("attempt"); query != "" {
		attempt, err = strconv.Atoi(query)
		if err != nil || attempt < 1 {
			executionError(w, execution.ErrInvalid)
			return true
		}
	}
	if current == 0 {
		writeJSON(w, 200, map[string]any{"attempts": 0, "attempt": 0, "text": "", "complete": false})
		return true
	}
	var exec, state, text string
	var exit sql.NullInt64
	if err = s.cfg.DB.QueryRow("SELECT execution_id,state,result_output,exit_code FROM execution_attempts WHERE order_id=? AND attempt=?", o.ID, attempt).Scan(&exec, &state, &text, &exit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = execution.ErrNotFound
		}
		executionError(w, err)
		return true
	}
	complete := state == "succeeded" || state == "failed" || state == "cancelled"
	if !complete {
		var b strings.Builder
		var after int64
		for {
			chunks, err := s.attempts.Output(exec, after)
			if err != nil {
				executionError(w, err)
				return true
			}
			for _, chunk := range chunks {
				b.WriteString(chunk.Chunk)
				after = chunk.Seq
			}
			if len(chunks) < 100 {
				break
			}
		}
		text = b.String()
	}
	out := map[string]any{"attempts": current, "attempt": attempt, "executionId": exec, "state": state, "text": text, "complete": complete}
	if complete && exit.Valid {
		out["exitCode"] = exit.Int64
	}
	writeJSON(w, 200, out)
	return true
}
