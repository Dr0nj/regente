// Package api — ARCH-4: transporte SSE (Server-Sent Events) para agentes.
//
// Terceiro transporte, além do WebSocket e do HTTP long-poll: um stream HTTP
// unidirecional server→agente (`text/event-stream`). Comparado ao long-poll,
// entrega o dispatch por PUSH IMEDIATO — sem a janela de ~25s em que um agente
// ocioso só "acorda" no fim do poll. Mantém o modelo OUTBOUND NAT-friendly (o
// agente abre a conexão) e reaproveita TODO o resto: o mesmo `agentBroker`
// registra o agente como `hub.Client`, então o dispatch continua saindo por
// PickAgent+Send sem tocar o scheduler, e os resultados voltam pelos MESMOS
// POST /api/agent/result e /output do long-poll. Ver docs/architecture-future.md §5.
package api

import (
	"net/http"
	"time"
)

// sseKeepalive — cadência do comentário de keep-alive. Menor que
// agentStaleAfter (60s) porque cada frame também re-toca o broker: sem re-tocar,
// o reaper removeria o agente (que num SSE não re-polla) e fecharia o stream.
const sseKeepalive = 20 * time.Second

// agentSSE — GET /api/agent/events?id=&caps=&env= : abre o stream de dispatch.
func (s *server) agentSSE(w http.ResponseWriter, r *http.Request) {
	p, ok := s.machineHandshake(w, r)
	if !ok {
		return
	}
	if s.agentBroker == nil {
		http.Error(w, "http transport disabled", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	client := s.agentBroker.touch(p, s)
	defer closeMachineStream(w, client)()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: não bufferizar o stream
	w.WriteHeader(http.StatusOK)
	// Comentário inicial: abre o stream no cliente e destrava proxies que seguram
	// o 1º byte.
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	ka := time.NewTicker(sseKeepalive)
	defer ka.Stop()
	for {
		select {
		case raw, ok := <-client.Send:
			if !s.machineValid(p) {
				return
			}
			if !ok {
				return // broker removeu o client (reaper/unregister) → fim do stream
			}
			if !s.agentBroker.keepAlive(client) {
				return
			}
			// SSE: um evento é "data: <linha>\n\n". O payload é JSON de 1 linha.
			if _, err := w.Write([]byte("data: ")); err != nil {
				return
			}
			_, _ = w.Write(raw)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-ka.C:
			if !s.machineValid(p) {
				return
			}
			// Heartbeat: re-toca o broker (senão o reaper mata o agente que não
			// re-polla) e envia um comentário para detectar conexão morta.
			if !s.agentBroker.keepAlive(client) {
				return
			}
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return // agente desconectou
		}
	}
}
