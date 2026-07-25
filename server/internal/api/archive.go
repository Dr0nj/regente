// ADV-5 — Archives/Retention: superfície de leitura dos archives NDJSON que o
// GC pós-daily (scheduler/archivegc.go) escreve em settings.archive_dir.
//
//	GET /api/archive          → lista os arquivos (dia, bytes, mtime) — admin-only
//	GET /api/archive/{file}   → baixa um NDJSON (streaming) — admin-only
//
// Admin-only como o /audit/export: um archive carrega output e snapshot de
// TODAS as folders — não passa pelo RBAC por team.
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// archiveFileRe — nomes que o GC gera (instances-YYYY-MM-DD[-N].ndjson). O
// download só aceita nomes neste formato: nada de path traversal.
var archiveFileRe = regexp.MustCompile(`^instances-\d{4}-\d{2}-\d{2}(-\d+)?\.ndjson$`)

// apiArchiveDir resolve settings.archive_dir (default ./archive) — mesma chave
// e mesmo default do scheduler.archiveDir.
func (s *server) apiArchiveDir() string {
	var v string
	_ = s.cfg.DB.QueryRow(`SELECT value FROM settings WHERE key='archive_dir'`).Scan(&v)
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return "archive"
}

func (s *server) listArchives(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := s.apiArchiveDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, 200, map[string]any{"dir": dir, "archives": []any{}})
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	type item struct {
		File       string `json:"file"`
		Day        string `json:"day"`
		SizeBytes  int64  `json:"sizeBytes"`
		ModifiedAt string `json:"modifiedAt"`
	}
	out := []item{}
	for _, e := range entries {
		if e.IsDir() || !archiveFileRe.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, item{
			File: e.Name(), Day: e.Name()[len("instances-") : len("instances-")+10],
			SizeBytes: info.Size(), ModifiedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	writeJSON(w, 200, map[string]any{"dir": dir, "archives": out})
}

func (s *server) downloadArchive(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	name := chi.URLParam(r, "file")
	if !archiveFileRe.MatchString(name) {
		http.Error(w, "invalid name (expected instances-YYYY-MM-DD.ndjson)", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.apiArchiveDir(), name)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "archive not found", http.StatusNotFound)
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer f.Close()
	var mt time.Time
	if info, err := f.Stat(); err == nil {
		mt = info.ModTime()
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, mt, f)
}
