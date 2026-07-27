// ARCH-3 — Lock-por-tick: guarda de ciclos de scheduling SOBREPOSTOS.
//
// Não é correção (o claim atômico de startInstance já garante no máximo 1
// execução por instance mesmo com N ticks concorrentes) — é HIGIENE: evita
// dois ciclos rodando scheduling redundante ao mesmo tempo. Ver a discussão em
// docs/architecture-future.md §4 ("Concorrência e HA — clássico vs. serverless").
//
// Duas camadas:
//   - EM-PROCESSO (sempre ativa): um guard atômico impede que os nudges
//     `go s.Tick()` (confirm/ingest/agent.changed/...) empilhem ciclos
//     concorrentes dentro do MESMO processo. Custo zero, DB-agnóstico.
//   - CROSS-PROCESSO (opt-in, só Postgres): um `pg_try_advisory_lock` de escopo
//     CURTO — adquirido no início do Tick e solto no fim, chave DISTINTA da
//     liderança (leader.DefaultAdvisoryKey) — impede que dois containers que
//     recebem `POST /scheduler/tick` ao mesmo tempo (cron externo dispara 2×, ou
//     ticks longos se cruzam) rodem scheduling em paralelo. Ligado por
//     EnableTickLock() no modo serverless (-scheduler=external). SQLite é
//     single-writer/nó único, então a camada em-processo já basta.
package scheduler

import (
	"context"
	"sync/atomic"

	"github.com/Dr0nj/regente-server/internal/db"
)

// tickAdvisoryKey — chave do advisory lock POR-TICK. Distinta da chave de
// LIDERANÇA (leader.DefaultAdvisoryKey = "RGNT") para não colidir com o lock de
// longa duração do líder. "RGTC" = ReGente TiCk.
const tickAdvisoryKey int64 = 0x5247_5443

// tickGuard serializa ticks sobrepostos (ver o cabeçalho do arquivo).
type tickGuard struct {
	busy   atomic.Bool // camada em-processo (sempre)
	db     *db.DB
	dbLock bool // liga a camada cross-processo (só faz efeito em Postgres)
}

// tryEnter adquire as camadas ativas. Devolve (ok, release): ok=false = já há um
// tick rodando (neste processo ou, com dbLock, em outro nó) e o caller deve
// PULAR — o próximo tick recupera (idempotente). release() sempre é seguro
// chamar (no-op quando ok=false).
func (g *tickGuard) tryEnter() (bool, func()) {
	if g == nil {
		return true, func() {}
	}
	if !g.busy.CompareAndSwap(false, true) {
		return false, func() {} // outro tick em-processo já corre
	}
	release := func() { g.busy.Store(false) }

	if !g.dbLock || g.db == nil || g.db.Dialect() != db.Postgres {
		return true, release
	}

	// Camada cross-processo: uma conexão dedicada segura o advisory lock enquanto
	// o tick roda; soltar no release fecha o lock e a conexão.
	conn, err := g.db.Raw().Conn(context.Background())
	if err != nil {
		return true, release // sem conexão pro lock: degrada pra camada em-processo
	}
	var got bool
	if err := conn.QueryRowContext(context.Background(),
		"SELECT pg_try_advisory_lock($1)", tickAdvisoryKey).Scan(&got); err != nil || !got {
		_ = conn.Close()
		release()
		return false, func() {} // outro nó está no tick
	}
	return true, func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", tickAdvisoryKey)
		_ = conn.Close()
		release()
	}
}
