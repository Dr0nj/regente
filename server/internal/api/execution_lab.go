package api

import (
	"encoding/json"
	"errors"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

func (s *server) labEnabled(w http.ResponseWriter, r *http.Request) bool {
	if s.attempts == nil || s.cfg.RuntimePolicy.Production() {
		http.NotFound(w, r)
		return false
	}
	return true
}
func (s *server) labAdmin(w http.ResponseWriter, r *http.Request) bool {
	return s.labEnabled(w, r) && s.requireAdmin(w, r)
}
func executionError(w http.ResponseWriter, err error) {
	status, message := http.StatusServiceUnavailable, "execution storage unavailable; retry with the same identity and key"
	switch {
	case errors.Is(err, execution.ErrConflict):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, execution.ErrCapacity):
		status, message = http.StatusTooManyRequests, err.Error()
	case errors.Is(err, execution.ErrInvalid):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, execution.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, execution.ErrOutputLimit):
		status, message = http.StatusRequestEntityTooLarge, err.Error()
	}
	writeJSON(w, status, map[string]any{"error": message})
}
func executionBody(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, execution.MaxOutputBytes+(1<<20)))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid execution body", http.StatusBadRequest)
		return false
	}
	return true
}
func (s *server) labCreateOrder(w http.ResponseWriter, r *http.Request) {
	if !s.labAdmin(w, r) {
		return
	}
	var b struct {
		SourceInstanceID string `json:"sourceInstanceId"`
		Key              string `json:"idempotencyKey"`
	}
	if !executionBody(w, r, &b) {
		return
	}
	o, err := s.attempts.CreateOrder(b.SourceInstanceID, b.Key)
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}
func (s *server) labOrder(w http.ResponseWriter, r *http.Request) {
	if !s.labAdmin(w, r) {
		return
	}
	o, err := s.attempts.Order(chi.URLParam(r, "id"))
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}
func (s *server) labStart(w http.ResponseWriter, r *http.Request) {
	if !s.labAdmin(w, r) {
		return
	}
	o, err := s.attempts.Order(chi.URLParam(r, "id"))
	if err != nil {
		executionError(w, err)
		return
	}
	if o.Runtime {
		executionError(w, execution.ErrConflict)
		return
	}
	var b struct {
		Agent  string `json:"agentId"`
		Key    string `json:"idempotencyKey"`
		Intent string `json:"intent"`
	}
	if !executionBody(w, r, &b) {
		return
	}
	a, err := s.attempts.Start(chi.URLParam(r, "id"), b.Agent, b.Key, b.Intent)
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}
func (s *server) labCancel(w http.ResponseWriter, r *http.Request) {
	if !s.labAdmin(w, r) {
		return
	}
	o, err := s.attempts.Order(chi.URLParam(r, "id"))
	if err != nil {
		executionError(w, err)
		return
	}
	if o.Runtime {
		executionError(w, execution.ErrConflict)
		return
	}
	a, err := s.attempts.Cancel(chi.URLParam(r, "id"))
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}
func (s *server) labAttempt(w http.ResponseWriter, r *http.Request) {
	if !s.labAdmin(w, r) {
		return
	}
	a, err := s.attempts.Attempt(chi.URLParam(r, "id"))
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}
func (s *server) labOutput(w http.ResponseWriter, r *http.Request) {
	if !s.labAdmin(w, r) {
		return
	}
	after := int64(0)
	var err error
	if raw := r.URL.Query().Get("after"); raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			executionError(w, execution.ErrInvalid)
			return
		}
	}
	out, err := s.attempts.Output(chi.URLParam(r, "id"), after)
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *server) labReconcile(w http.ResponseWriter, r *http.Request) {
	if !s.labAdmin(w, r) {
		return
	}
	n, err := s.attempts.Reconcile()
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"uncertain": n})
}

func (s *server) executionMachine(w http.ResponseWriter, r *http.Request, handshake bool) (*machinePrincipal, bool) {
	if !s.cfg.RuntimePolicy.Durable() {
		if !s.labEnabled(w, r) {
			return nil, false
		}
	} else if s.attempts == nil {
		http.Error(w, "durable runtime unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	var p *machinePrincipal
	var ok bool
	if handshake {
		p, ok = s.machineHandshake(w, r)
		if !ok {
			return nil, false
		}
		if r.URL.Query().Get("protocol") != "2" {
			http.Error(w, "protocol 2 is required", http.StatusUpgradeRequired)
			return nil, false
		}
	} else {
		p, ok = s.machineAuth(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return nil, false
		}
	}
	if !slices.Contains(strings.Split(p.Capabilities, ","), execution.Capability) {
		http.Error(w, "EXECUTION_V2 capability is required", http.StatusForbidden)
		return nil, false
	}
	return p, true
}
func (s *server) executionPoll(w http.ResponseWriter, r *http.Request) {
	p, ok := s.executionMachine(w, r, true)
	if !ok {
		return
	}
	if _, err := s.attempts.Reconcile(); err != nil {
		executionError(w, err)
		return
	}
	if s.cfg.RuntimePolicy.Durable() {
		if r.URL.Query().Get("journal") != "1" || r.URL.Query().Get("ver") == "" {
			http.Error(w, "journal version 1 and agent version are required", http.StatusUpgradeRequired)
			return
		}
		q := r.URL.Query()
		if _, err := s.cfg.DB.Exec("INSERT INTO agents(id,os,arch,host,version,capabilities,started_at,connected_at,first_seen,last_seen_at,online) VALUES(?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1) ON CONFLICT(id) DO UPDATE SET os=excluded.os,arch=excluded.arch,host=excluded.host,version=excluded.version,capabilities=excluded.capabilities,last_seen_at=CURRENT_TIMESTAMP,online=1", p.AgentID, q.Get("os"), q.Get("arch"), q.Get("host"), q.Get("ver"), p.Capabilities); err != nil {
			executionError(w, err)
			return
		}
	}
	available := r.URL.Query().Get("available")
	if available != "" && available != "0" && available != "1" {
		executionError(w, execution.ErrInvalid)
		return
	}
	if s.cfg.RuntimePolicy.Durable() {
		slots, pending := 1, 1 // Agente anterior: admissão conservadora, sem inventar capacidade.
		for name, dest := range map[string]*int{"slots": &slots, "pendingLimit": &pending} {
			if raw := r.URL.Query().Get(name); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil {
					executionError(w, execution.ErrInvalid)
					return
				}
				*dest = n
			}
		}
		if err := s.cfg.Scheduler.ReportAgentCapacity(p.AgentID, slots, pending, available != "0"); err != nil {
			executionError(w, err)
			return
		}
	}
	msg, err := s.attempts.ClaimCapacity(p.AgentID, available != "0")
	if err != nil {
		executionError(w, err)
		return
	}
	if !s.machineValid(p) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if msg == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Write/Publish não confirma aceite; apenas ACK explícito fecha a dispatch outbox.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, msg)
}
func (s *server) executionAck(w http.ResponseWriter, r *http.Request) {
	p, ok := s.executionMachine(w, r, false)
	if !ok {
		return
	}
	var b struct {
		execution.Identity
		Kind   string `json:"kind"`
		Reason string `json:"reason,omitempty"`
	}
	if !executionBody(w, r, &b) {
		return
	}
	b.AgentID = p.AgentID
	out, err := s.attempts.AcknowledgeReason(b.Identity, b.Kind, b.Reason)
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *server) executionResult(w http.ResponseWriter, r *http.Request) {
	p, ok := s.executionMachine(w, r, false)
	if !ok {
		return
	}
	var b struct {
		execution.Identity
		ExitCode *int    `json:"exitCode"`
		Output   *string `json:"output"`
	}
	if !executionBody(w, r, &b) {
		return
	}
	if b.ExitCode == nil || b.Output == nil {
		executionError(w, execution.ErrInvalid)
		return
	}
	b.AgentID = p.AgentID
	out, err := s.attempts.Complete(execution.Result{Identity: b.Identity, ExitCode: *b.ExitCode, Output: *b.Output})
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *server) executionOutput(w http.ResponseWriter, r *http.Request) {
	p, ok := s.executionMachine(w, r, false)
	if !ok {
		return
	}
	var b execution.Output
	if !executionBody(w, r, &b) {
		return
	}
	b.AgentID = p.AgentID
	out, err := s.attempts.Append(b)
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
