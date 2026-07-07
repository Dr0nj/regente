// Package audit — exportação de eventos de auditoria para SIEM (segurança enterprise).
//
// Emite eventos estruturados (JSON, UMA linha por evento) para um io.Writer (default
// stderr — coletado por agentes de log/SIEM via stdout do container) e, opcionalmente,
// faz POST para um endpoint HTTP de SIEM (Splunk HEC / Elastic / genérico). Best-effort:
// nunca bloqueia nem derruba o fluxo principal.
package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// Event — evento de auditoria de segurança.
type Event struct {
	TS      string `json:"ts"`
	Type    string `json:"type"`             // ex.: "auth.login", "definition.write"
	Actor   string `json:"actor"`            // usuário ou "system"
	Action  string `json:"action"`           // ex.: "login", "save", "delete"
	Target  string `json:"target,omitempty"` // recurso afetado (ex.: "team/jobId")
	Outcome string `json:"outcome"`          // "success" | "failure"
	IP      string `json:"ip,omitempty"`
	Detail  string `json:"detail,omitempty"` // E2 — contexto extra (ex.: settings "chave: de→para"); NUNCA valores secretos
}

// Sink emite eventos. Construído uma vez no boot e compartilhado.
type Sink struct {
	url    string
	client *http.Client
	mu     sync.Mutex
	out    io.Writer
}

// New cria o sink. siemURL vazio = só JSON em stderr (sem POST HTTP).
func New(siemURL string) *Sink {
	return &Sink{url: siemURL, client: &http.Client{Timeout: 5 * time.Second}, out: os.Stderr}
}

// Emit grava o evento como uma linha JSON (TS preenchido se vazio) e, se houver URL de
// SIEM, faz POST assíncrono. Seguro com receiver nil (no-op).
func (s *Sink) Emit(e Event) {
	if s == nil {
		return
	}
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	s.mu.Lock()
	_, _ = s.out.Write(append(b, '\n'))
	s.mu.Unlock()

	if s.url != "" {
		go func() {
			req, err := http.NewRequest(http.MethodPost, s.url, bytes.NewReader(b))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			if resp, err := s.client.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}()
	}
}
