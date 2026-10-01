package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/go-chi/chi/v5"
)

type draftContextKey struct{}

func draftError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	if errors.Is(err, storage.ErrDraftNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, storage.ErrDraftConflict) || errors.Is(err, storage.ErrDraftPublishing) {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]any{"error": "draft", "message": err.Error()})
}
func draftETag(s *storage.DesignSession) string {
	return "\"" + strconv.FormatInt(s.Revision, 10) + "\""
}

// Todas as mutações de Design usam o mesmo checkpoint CAS e cache privado.
// Nenhum 2xx de gravação é enviado antes da confirmação durável.
func (s *server) draftMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Sessions == nil {
			http.Error(w, "design sessions not configured", 503)
			return
		}
		sid := chi.URLParam(r, "sid")
		meta, err := s.cfg.Sessions.Metadata(sid)
		if err != nil {
			if errors.Is(err, storage.ErrDraftNotFound) && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/publish") {
				closed, result, receiptErr := s.cfg.Sessions.Published(sid)
				if receiptErr == nil {
					u, _ := auth.FromContext(r.Context())
					if actorFromCtx(r) != closed.Actor && (u == nil || !u.Role.CanAdmin()) {
						http.Error(w, "forbidden: draft belongs to another user", 403)
						return
					}
					expected := r.Header.Get("If-Match")
					if expected == "" {
						http.Error(w, "If-Match draft revision is required", 428)
						return
					}
					previous := *closed
					previous.Revision--
					if expected != draftETag(closed) && expected != draftETag(&previous) {
						draftError(w, storage.ErrDraftConflict)
						return
					}
					for _, folder := range closed.Folders {
						can, e := auth.CanWriteFolder(s.cfg.DB, u, folder)
						if e != nil {
							draftError(w, e)
							return
						}
						if !can {
							http.Error(w, "forbidden: folder access revoked", 403)
							return
						}
					}
					w.Header().Set("ETag", draftETag(closed))
					writeJSON(w, 200, result)
					return
				}
				if !errors.Is(receiptErr, storage.ErrDraftNotFound) {
					draftError(w, receiptErr)
					return
				}
			}
			draftError(w, err)
			return
		}
		u, _ := auth.FromContext(r.Context())
		actor := actorFromCtx(r)
		if actor != meta.Actor && (u == nil || !u.Role.CanAdmin()) {
			http.Error(w, "forbidden: draft belongs to another user", 403)
			return
		}
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		if mutation {
			r.Body = http.MaxBytesReader(w, r.Body, 24<<20)
		}
		root := strings.HasSuffix(r.URL.Path, "/"+sid)
		publish := strings.HasSuffix(r.URL.Path, "/publish")
		cancel := strings.HasSuffix(r.URL.Path, "/publication/cancel")
		if mutation && s.cfg.Sessions.Shared() {
			if r.Header.Get("If-Match") == "" {
				writeJSON(w, 428, map[string]string{"message": "If-Match draft revision is required; reload the draft"})
				return
			}
			if r.Header.Get("If-Match") != draftETag(meta) {
				draftError(w, storage.ErrDraftConflict)
				return
			}
			if meta.State != "active" && meta.State != "legacy" && !publish && !cancel {
				draftError(w, storage.ErrDraftPublishing)
				return
			}
			// Revalida todas as folders, inclusive depois de revogar ACL.
			if u != nil {
				for _, folder := range append(append([]string{}, meta.Folders...), meta.NewFolders...) {
					can, e := auth.CanWriteFolder(s.cfg.DB, u, folder)
					if e != nil {
						draftError(w, e)
						return
					}
					if !can {
						http.Error(w, "forbidden: folder access revoked", 403)
						return
					}
				}
			}
		}
		if !mutation && !root && u != nil {
			for _, folder := range append(append([]string{}, meta.Folders...), meta.NewFolders...) {
				can, e := auth.CanReadFolder(s.cfg.DB, u, folder)
				if e != nil {
					draftError(w, e)
					return
				}
				if !can {
					http.Error(w, "forbidden: folder access revoked", 403)
					return
				}
			}
		}
		sess := meta
		cleanup := func() {}
		if !root {
			sess, cleanup, err = s.cfg.Sessions.Open(sid)
			if err != nil {
				draftError(w, err)
				return
			}
		}
		defer cleanup()
		// Entre a leitura de metadados e Open outro nó pode ter confirmado uma revisão.
		if mutation && s.cfg.Sessions.Shared() && sess.Revision != meta.Revision {
			draftError(w, storage.ErrDraftConflict)
			return
		}
		if err = s.cfg.Sessions.Touch(sess); err != nil {
			draftError(w, err)
			return
		}
		response := httptest.NewRecorder()
		next.ServeHTTP(response, r.WithContext(context.WithValue(r.Context(), draftContextKey{}, sess)))
		if mutation && response.Code >= 200 && response.Code < 300 && !root && !publish && !cancel {
			if err = s.cfg.Sessions.Checkpoint(sess, actor, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/api/design/sessions/"+sid)); err != nil {
				massUndo.clear(sid)
				draftError(w, err)
				return
			}
		}
		for key, values := range response.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.Header().Set("ETag", draftETag(sess))
		w.Header().Set("X-Draft-Version", strconv.FormatInt(sess.Revision, 10))
		w.WriteHeader(response.Code)
		_, _ = io.Copy(w, response.Body)
	})
}

// Exportações contêm a base inteira do repo e são administrativas.
func (s *server) exportDraft(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if u == nil || !u.Role.CanAdmin() {
		http.Error(w, "forbidden: admin only", 403)
		return
	}
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	export, err := s.cfg.Sessions.Export(sess)
	if err != nil {
		draftError(w, err)
		return
	}
	writeJSON(w, 200, export)
}
func (s *server) importDraft(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if u == nil || !u.Role.CanAdmin() {
		http.Error(w, "forbidden: admin only", 403)
		return
	}
	if s.cfg.Sessions == nil {
		http.Error(w, "design sessions not configured", 503)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 24<<20)
	var export storage.DraftExport
	if err := json.NewDecoder(r.Body).Decode(&export); err != nil {
		http.Error(w, "invalid draft export: "+err.Error(), 400)
		return
	}
	sess, err := s.cfg.Sessions.Import(export, actorFromCtx(r))
	if err != nil {
		draftError(w, err)
		return
	}
	w.Header().Set("ETag", draftETag(sess))
	writeJSON(w, 201, sess)
}
func (s *server) cancelDraftPublication(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Sessions.CancelPublication(sess, actorFromCtx(r)); err != nil {
		draftError(w, err)
		return
	}
	writeJSON(w, 200, sess)
}

func (s *server) setSessionFolderLayout(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if !s.requireFolderWrite(w, r, name) {
		return
	}
	var layout storage.FolderLayout
	if err := json.NewDecoder(r.Body).Decode(&layout); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if layout.Columns < 0 || layout.Columns > 40 || layout.MaxRows < 0 || layout.MaxRows > 200 {
		http.Error(w, "layout out of range", 400)
		return
	}
	var value *storage.FolderLayout
	if layout.Columns > 0 || layout.MaxRows > 0 {
		value = &layout
	}
	if err := sess.Store.SetFolderLayout(name, value); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "layout": value})
}
