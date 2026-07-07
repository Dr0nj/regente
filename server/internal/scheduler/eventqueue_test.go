package scheduler

// E4 — fila assíncrona de eventos: zero perdas (fila cheia degrada pro
// síncrono), ordem por instance preservada no caminho da fila, flush final
// no Stop() e write síncrono determinístico com a fila desligada.

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Aceite da spec: 10k emits concorrentes → todas as linhas no banco e a ordem
// POR INSTANCE preservada (ids crescentes na ordem de emissão de cada worker).
func TestEventQueue_10kConcorrentesSemPerdaEOrdemPorInstance(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de volume em -short")
	}
	s := newTestScheduler(t)
	s.StartEventQueue()

	const workers, perWorker = 20, 500 // 10k no total; 1 instance por worker
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			inst := fmt.Sprintf("inst-%02d", w)
			for i := 0; i < perWorker; i++ {
				s.emitEvent(inst, "seq", "t", fmt.Sprintf("%05d", i))
			}
		}(w)
	}
	wg.Wait()
	s.Stop() // drena + flush final — depois disso TUDO tem que estar no banco

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE kind='seq'`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != workers*perWorker {
		t.Fatalf("esperava %d eventos no banco, veio %d (perda!)", workers*perWorker, total)
	}

	// Ordem por instance: percorrendo por id crescente, o message (sequência do
	// worker) tem que ser estritamente crescente dentro de cada instance.
	rows, err := s.db.Query(`SELECT instance_id, message FROM instance_events WHERE kind='seq' ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	last := map[string]string{}
	for rows.Next() {
		var inst, msg string
		if rows.Scan(&inst, &msg) != nil {
			continue
		}
		if prev, ok := last[inst]; ok && msg <= prev {
			t.Fatalf("ordem violada em %s: %s veio depois de %s", inst, msg, prev)
		}
		last[inst] = msg
	}
}

// Fila CHEIA nunca perde: canal minúsculo SEM writer (determinístico — nada
// drena) → o excedente degrada pro INSERT síncrono na hora; ligando o writer
// depois, o Stop() drena o resto (zero perdas de ponta a ponta).
func TestEventQueue_CheiaDegradaProSincrono(t *testing.T) {
	origFlush := eventFlushEvery
	eventFlushEvery = time.Hour // flush só no drain do Stop
	defer func() { eventFlushEvery = origFlush }()

	s := newTestScheduler(t)
	ch := make(chan eventRec, 2)
	s.mu.Lock()
	s.eventCh = ch // fila ativa, writer AINDA não — canal enche e fica cheio
	s.mu.Unlock()

	for i := 0; i < 10; i++ {
		s.emitEvent("cheia-1", "burst", "t", fmt.Sprintf("%d", i))
	}
	var sync int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE kind='burst'`).Scan(&sync)
	if sync != 8 {
		t.Fatalf("esperava 8 writes síncronos de degradação (canal cheio com 2), veio %d", sync)
	}
	if len(ch) != 2 {
		t.Fatalf("canal deveria segurar 2 pendentes, veio %d", len(ch))
	}

	// Agora o writer entra e o Stop() drena os 2 pendentes no flush final.
	s.wg.Add(1)
	go s.eventWriter(ch)
	s.Stop()
	var total int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE kind='burst'`).Scan(&total)
	if total != 10 {
		t.Fatalf("esperava 10 eventos após o flush final (zero perdas), veio %d", total)
	}
}

// Fila desligada (default dos testes/modo external) = write síncrono imediato.
func TestEventQueue_DesligadaEhSincrona(t *testing.T) {
	s := newTestScheduler(t)
	s.emitEvent("solo-1", "sync", "t", "m")
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE kind='sync'`).Scan(&n)
	if n != 1 {
		t.Fatalf("sem fila o write deveria ser síncrono imediato, veio %d linhas", n)
	}
	if depth, active := s.EventQueueDepth(); active || depth != 0 {
		t.Fatalf("fila desligada deveria reportar active=false depth=0, veio %v %d", active, depth)
	}
}

// Lote estoura eventBatchMax → múltiplos INSERTs multi-values, nada some.
func TestEventQueue_LotesPequenos(t *testing.T) {
	origBatch := eventBatchMax
	eventBatchMax = 3
	defer func() { eventBatchMax = origBatch }()

	s := newTestScheduler(t)
	s.StartEventQueue()
	for i := 0; i < 10; i++ {
		s.emitEvent("lote-1", "lote", "t", fmt.Sprintf("%d", i))
	}
	s.Stop()
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE kind='lote'`).Scan(&n)
	if n != 10 {
		t.Fatalf("esperava 10 eventos com lotes de 3, veio %d", n)
	}
}
