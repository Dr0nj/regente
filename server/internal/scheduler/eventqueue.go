// E4 — Fila assíncrona de eventos de instance.
//
// emitEvent fazia um INSERT síncrono no caminho do tick/dispatch; no Postgres
// sob carga extrema esse write por evento compete com o scheduling. Com a fila:
// o hot path só faz um send não-bloqueante num canal buffered e uma goroutine
// writer única grava em LOTE (INSERT multi-values) — flush a cada 250ms OU 500
// eventos, o que vier primeiro (mesmo espírito do insertDailyBatch).
//
// Garantias:
//   - NUNCA perde evento: fila cheia degrada pro INSERT síncrono de antes.
//   - Ordem POR INSTANCE preservada no caminho da fila (canal único FIFO +
//     writer único + lote na ordem de chegada). Única fronteira sem garantia:
//     um write síncrono de degradação pode ganhar id menor que eventos da MESMA
//     instance ainda no buffer — só acontece com a fila cheia (10k pendentes) e
//     o ts continua correto.
//   - Flush final no shutdown: Stop() fecha `quit`, o writer drena o canal e
//     grava o resto ANTES do s.wg.Wait() retornar (nada escapa pro teardown —
//     ver o histórico do flake do TempDir).
//
// A fila é OPT-IN por StartEventQueue(): o main liga no modo -scheduler=internal.
// No modo external (serverless scale-to-zero) fica DESLIGADA de propósito — o
// processo pode ser congelado/morto entre requests e eventos bufferizados em
// memória morreriam junto; síncrono é o correto lá. Testes que não ligam a fila
// seguem com o write síncrono determinístico.
package scheduler

import (
	"log"
	"strings"
	"time"
)

// Capacidade/limiares da fila. Vars (não const) p/ os testes exercitarem
// fila-cheia e lotes pequenos sem enfileirar dezenas de milhares de linhas.
var (
	eventQueueCap   = 10_000
	eventBatchMax   = 500
	eventFlushEvery = 250 * time.Millisecond
)

// eventRec — um evento decidido, aguardando o INSERT em lote.
type eventRec struct{ instanceID, kind, actor, message string }

// StartEventQueue liga a fila assíncrona (idempotente). Chamar ANTES de servir
// tráfego; o writer respeita o ciclo de vida quit/wg do Scheduler (Stop() drena).
func (s *Scheduler) StartEventQueue() {
	s.mu.Lock()
	if s.eventCh != nil {
		s.mu.Unlock()
		return
	}
	ch := make(chan eventRec, eventQueueCap)
	s.eventCh = ch
	s.mu.Unlock()

	s.wg.Add(1)
	go s.eventWriter(ch)
}

// eventQueue devolve o canal ativo (nil = fila desligada, write síncrono).
func (s *Scheduler) eventQueue() chan eventRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eventCh
}

// EventQueueDepth — profundidade atual da fila p/ o /metrics
// (regente_event_queue_depth). active=false quando a fila está desligada.
func (s *Scheduler) EventQueueDepth() (depth int, active bool) {
	ch := s.eventQueue()
	if ch == nil {
		return 0, false
	}
	return len(ch), true
}

// eventWriter — goroutine única que agrega e grava em lote.
func (s *Scheduler) eventWriter(ch chan eventRec) {
	defer s.wg.Done()
	ticker := time.NewTicker(eventFlushEvery)
	defer ticker.Stop()
	buf := make([]eventRec, 0, eventBatchMax)
	flush := func() {
		if len(buf) > 0 {
			s.writeEventBatch(buf)
			buf = buf[:0]
		}
	}
	for {
		select {
		case rec := <-ch:
			buf = append(buf, rec)
			if len(buf) >= eventBatchMax {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.quit:
			// Drena o que ainda está no canal e dá o flush final. Producers
			// concorrentes com o shutdown caem no síncrono (eventQueue ainda
			// devolve o canal, mas o default do select cobre canal cheio; um
			// send que entre durante o drain é gravado aqui mesmo).
			for {
				select {
				case rec := <-ch:
					buf = append(buf, rec)
					if len(buf) >= eventBatchMax {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// writeEventBatch grava um lote num único INSERT multi-values (ordem do slice
// = ordem de chegada = ordem dos ids). Falha loga e segue (best-effort, igual
// ao emitEvent clássico).
func (s *Scheduler) writeEventBatch(batch []eventRec) {
	var sb strings.Builder
	sb.WriteString(`INSERT INTO instance_events(instance_id, kind, actor, message) VALUES `)
	args := make([]any, 0, len(batch)*4)
	for i, rec := range batch {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?,?,?,?)")
		args = append(args, rec.instanceID, rec.kind, rec.actor, rec.message)
	}
	if _, err := s.db.Exec(sb.String(), args...); err != nil {
		log.Printf("[scheduler] event batch (%d eventos): %v", len(batch), err)
	}
}
