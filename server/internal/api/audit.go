// Package api — F13.5 audit binding for definition writes.
//
// Cada save/delete de definition emite uma entrada em `definition_audit`
// com {actor, action, team, id, git_commit_sha, pr_number, pr_url, branch, mode}.
// Endpoint GET /api/definitions/{team}/{id}/audit retorna histórico.
package api

import (
	"net/http"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/go-chi/chi/v5"
)

// recordDefinitionAudit insere uma linha em definition_audit.
// Falha em silêncio (loga, não bloqueia o write).
func (s *server) recordDefinitionAudit(actor, action, team, defID string, pr *storage.PRResult) {
	if s.cfg.DB == nil {
		return
	}
	var (
		sha    string
		num    int
		url    string
		branch string
		mode   string
	)
	if pr != nil {
		sha = pr.CommitSHA
		num = pr.PRNumber
		url = pr.PRURL
		branch = pr.Branch
		mode = pr.Mode
	}
	_, err := s.cfg.DB.Exec(`
		INSERT INTO definition_audit (actor, action, team, definition_id, git_commit_sha, pr_number, pr_url, branch, mode)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, actor, action, team, defID, nullable(sha), nullableInt(num), nullable(url), nullable(branch), nullable(mode))
	if err != nil {
		// não-fatal: log
		writeLogf("[audit] insert failed: %v", err)
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullableInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// listDefinitionAudit — GET /api/definitions/{team}/{id}/audit
func (s *server) listDefinitionAudit(w http.ResponseWriter, r *http.Request) {
	team := chi.URLParam(r, "team")
	id := chi.URLParam(r, "id")
	// gating: precisa ler a folder.
	if u, ok := auth.FromContext(r.Context()); ok && u != nil {
		canRead, _ := auth.CanReadFolder(s.cfg.DB, u, team)
		if !canRead {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	rows, err := s.cfg.DB.Query(`
		SELECT id, ts, actor, action, team, COALESCE(definition_id,''),
		       COALESCE(git_commit_sha,''), COALESCE(pr_number,0),
		       COALESCE(pr_url,''), COALESCE(branch,''), COALESCE(mode,'')
		FROM definition_audit
		WHERE team = ? AND definition_id = ?
		ORDER BY ts DESC
		LIMIT 200
	`, team, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type entry struct {
		ID           int    `json:"id"`
		TS           string `json:"ts"`
		Actor        string `json:"actor"`
		Action       string `json:"action"`
		Team         string `json:"team"`
		DefinitionID string `json:"definitionId"`
		GitCommitSHA string `json:"gitCommitSha,omitempty"`
		PRNumber     int    `json:"prNumber,omitempty"`
		PRURL        string `json:"prUrl,omitempty"`
		Branch       string `json:"branch,omitempty"`
		Mode         string `json:"mode,omitempty"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Team, &e.DefinitionID,
			&e.GitCommitSHA, &e.PRNumber, &e.PRURL, &e.Branch, &e.Mode); err != nil {
			continue
		}
		out = append(out, e)
	}
	writeJSON(w, 200, out)
}
