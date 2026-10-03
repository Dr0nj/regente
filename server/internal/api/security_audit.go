package api

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// Receiver por requisição; nunca muda o DB compartilhado entre usuários.
func (s *server) scoped(handler func(*server, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := make([]byte, 16)
		if _, err := rand.Read(requestID); err != nil {
			http.Error(w, "Request identity unavailable", http.StatusServiceUnavailable)
			return
		}
		clone := *s
		identity := db.AuditIdentity{Request: hex.EncodeToString(requestID), Actor: "anonymous", Route: r.Method + " " + r.URL.Path, IP: clientIP(r)}
		// Tickets/quick-action tokens não podem entrar na trilha como parte do path.
		if strings.HasPrefix(r.URL.Path, "/qa/") {
			identity.Route = r.Method + " /qa/{token}"
		}
		if u, ok := auth.FromContext(r.Context()); ok {
			identity.Actor = u.Username
		}
		if s.cfg.DB != nil {
			clone.cfg.DB = s.cfg.DB.WithAuditIdentity(identity)
		}
		if s.attempts != nil {
			engine := *s.attempts
			engine.DB = clone.cfg.DB
			clone.attempts = &engine
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			if err := clone.cfg.DB.AuditObservation("request.intent", "started"); err != nil {
				http.Error(w, "Mandatory audit unavailable; action refused", http.StatusServiceUnavailable)
				return
			}
		}
		handler(&clone, &auditResponse{ResponseWriter: w, DB: clone.cfg.DB, mutation: r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete}, r)
	}
}

// Denials are persisted before responding. A storage failure returns 503.
type auditResponse struct {
	http.ResponseWriter
	DB       *db.DB
	status   int
	refused  bool
	mutation bool
}

func (w *auditResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	event := ""
	if w.mutation {
		event = "request.result"
	}
	if status == 401 || status == 403 {
		event = "access.denied"
	}
	if event != "" {
		if err := w.DB.AuditObservation(event, strconv.Itoa(status)); err != nil {
			w.refused = true
			status = 503
		}
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *auditResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if w.refused {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}
func (w *auditResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *server) securityAudit(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	after := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			http.Error(w, "Invalid audit cursor", http.StatusBadRequest)
			return
		}
	}
	records, err := s.cfg.DB.AuditRecords(after, 500)
	if err != nil {
		http.Error(w, "Audit unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, 200, records)
}
func (s *server) securityAuditStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var pending, dead, head int64
	if err := s.cfg.DB.QueryRow("SELECT COUNT(*),COALESCE(SUM(dead_letter),0) FROM audit_delivery WHERE acknowledged=0").Scan(&pending, &dead); err != nil {
		http.Error(w, "Audit unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.cfg.DB.QueryRow("SELECT seq FROM audit_head WHERE id=1").Scan(&head); err != nil {
		http.Error(w, "Audit unavailable", http.StatusServiceUnavailable)
		return
	}
	integrity := "verified"
	if s.cfg.DB.VerifyAudit() != nil {
		integrity = "failed"
	}
	writeJSON(w, 200, map[string]any{"pending": pending, "deadLetters": dead, "head": head, "integrity": integrity})
}
func (s *server) retrySecurityAudit(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		http.Error(w, "Audit unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()
	if err = tx.AuditAdministrative("export.retry"); err == nil {
		_, err = tx.Exec("UPDATE audit_delivery SET dead_letter=0,attempts=0,next_at=0,error_code='' WHERE seq=(SELECT MIN(seq) FROM audit_delivery WHERE acknowledged=0)")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		http.Error(w, "Audit unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(204)
}

func (w *auditResponse) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *auditResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (s *server) recordAccessDenial(r *http.Request) error {
	identity := db.AuditIdentity{Actor: "anonymous", Route: r.Method + " " + r.URL.Path, IP: clientIP(r)}
	if u, ok := auth.FromContext(r.Context()); ok {
		identity.Actor = u.Username
	}
	return s.cfg.DB.WithAuditIdentity(identity).AuditObservation("access.denied", "403")
}
