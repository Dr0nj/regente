// Package api — Fase 2 (serverless): transporte HTTP long-poll para agentes.
//
// Alternativa stateless ao WebSocket: o agente faz long-poll em
// GET /api/agent/poll (bloqueia ~25s esperando dispatch) e devolve o resultado
// por POST /api/agent/result (+ stream interino por POST /api/agent/output).
// Mantém o modelo OUTBOUND (NAT-friendly) e reaproveita TODO o roteamento de
// dispatch existente: o broker registra um hub.Client com canal Send, então
// scheduler.startInstance despacha via PickAgent + Send sem qualquer mudança.
//
// Por que isso destrava serverless: sem conexão persistente no processo, o
// control plane pode rodar atrás de runtimes scale-to-zero. Ver
// docs/arquitetura-futuro.md (Fase 2).
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
)

const (
	agentPollWait   = 25 * time.Second // janela de bloqueio do long-poll
	agentStaleAfter = 60 * time.Second // sem poll por mais que isto → reaped
)

type pollAgent struct {
	client   *hub.Client
	lastSeen time.Time
}

// agentBroker registra agentes long-poll como hub.Clients (canal Send) e os
// remove quando param de pollar (reaper), já que não há evento de "disconnect"
// como no WebSocket.
type agentBroker struct {
	mu     sync.Mutex
	agents map[string]*pollAgent
	hub    *hub.Hub
}

func newAgentBroker(h *hub.Hub) *agentBroker {
	b := &agentBroker{agents: map[string]*pollAgent{}, hub: h}
	go b.reap()
	return b
}

// touch devolve o hub.Client do agente, registrando-o na primeira vez. As
// capabilities/env são fixadas na criação (evita corrida com PickAgent, que lê
// sob o lock do hub).
func (b *agentBroker) touch(id string, caps []string, env string) *hub.Client {
	b.mu.Lock()
	defer b.mu.Unlock()
	pa, ok := b.agents[id]
	if !ok {
		c := &hub.Client{
			ID:           id,
			Kind:         hub.ClientAgent,
			Send:         make(chan []byte, 64),
			Capabilities: caps,
			Environment:  env, // ADV-2 — mesmo label do transporte WS
		}
		b.hub.Register(c)
		pa = &pollAgent{client: c}
		b.agents[id] = pa
	}
	pa.lastSeen = time.Now()
	return pa.client
}

func (b *agentBroker) reap() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		b.mu.Lock()
		for id, pa := range b.agents {
			if now.Sub(pa.lastSeen) > agentStaleAfter {
				b.hub.Unregister(pa.client) // fecha Send
				delete(b.agents, id)
			}
		}
		b.mu.Unlock()
	}
}

func (s *server) agentAuthOK(r *http.Request) bool {
	return s.agentTokenValid(auth.ExtractToken(r)) || s.wsTokenOK(r)
}

// GET /api/agent/poll?id=<id>&caps=COMMAND,SCRIPT — long-poll por dispatch.
func (s *server) agentPoll(w http.ResponseWriter, r *http.Request) {
	if !s.agentAuthOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.agentBroker == nil {
		http.Error(w, "http transport disabled", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	var caps []string
	if c := r.URL.Query().Get("caps"); c != "" {
		caps = strings.Split(c, ",")
	}
	client := s.agentBroker.touch(id, caps, r.URL.Query().Get("env"))
	select {
	case raw, ok := <-client.Send:
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	case <-time.After(agentPollWait):
		w.WriteHeader(http.StatusNoContent) // nada pendente; agente re-polla
	case <-r.Context().Done():
		// cliente desistiu (timeout/reconnect); nada a fazer
	}
}

// POST /api/agent/result {instanceId, exitCode, output} — finaliza a instance.
func (s *server) agentResult(w http.ResponseWriter, r *http.Request) {
	if !s.agentAuthOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var ev struct {
		InstanceID string `json:"instanceId"`
		ExitCode   int    `json:"exitCode"`
		Output     string `json:"output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil || ev.InstanceID == "" {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	status := domain.StatusOK
	if ev.ExitCode != 0 {
		status = domain.StatusNotOK
	}
	s.cfg.Scheduler.FinishInstance(ev.InstanceID, status, ev.ExitCode, ev.Output)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/agent/output {instanceId, chunk} — stream interino de stdout/stderr.
func (s *server) agentOutput(w http.ResponseWriter, r *http.Request) {
	if !s.agentAuthOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var ev struct {
		InstanceID string `json:"instanceId"`
		Chunk      string `json:"chunk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if ev.InstanceID != "" && ev.Chunk != "" {
		s.cfg.Scheduler.EmitEvent(ev.InstanceID, "output", "agent", strings.TrimRight(ev.Chunk, "\r\n"))
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
