// Package leader — G1: eleição de líder para HA do scheduler.
//
// Só o nó líder materializa a daily e roda o tick de dispatch; os demais nós
// servem a API e ficam prontos para assumir. Duas implementações:
//
//   - SingleNode: sempre líder. Usado com SQLite (nó único, single-writer).
//   - PgAdvisory: usa pg_advisory_lock no Postgres. Vários nós disputam a MESMA
//     chave; quem segura o lock (numa conexão dedicada) é o líder. Se o líder
//     cai, a sessão Postgres encerra, o lock é liberado e outro nó assume no
//     próximo tick.
//
// Observação de segurança: mesmo que dois nós despachem por engano, o claim
// atômico de startInstance (UPDATE ... WHERE status='WAITING') garante no
// máximo uma execução por instance. A eleição evita a corrida na daily.
package leader

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

// Elector é a interface mínima consumida pelo scheduler (via AttachLeader).
type Elector interface {
	IsLeader() bool
}

// SingleNode é sempre líder (SQLite / nó único).
type SingleNode struct{}

func (SingleNode) IsLeader() bool        { return true }
func (SingleNode) Start(context.Context) {}
func (SingleNode) Describe() string      { return "single-node (always leader)" }

// DefaultAdvisoryKey — chave do advisory lock do scheduler. Arbitrária mas fixa;
// todos os nós do mesmo cluster devem usar a mesma.
const DefaultAdvisoryKey int64 = 0x5247_4E54 // "RGNT"

// PgAdvisory dá liderança a um único nó via pg_advisory_lock no Postgres.
type PgAdvisory struct {
	raw      *sql.DB
	key      int64
	interval time.Duration

	mu     sync.RWMutex
	leader bool
	conn   *sql.Conn // conexão dedicada que segura o lock enquanto for líder
}

// NewPgAdvisory cria o eleitor para Postgres. `raw` deve ser o *sql.DB cru
// (db.DB.Raw()). interval=0 usa 5s.
func NewPgAdvisory(raw *sql.DB, key int64, interval time.Duration) *PgAdvisory {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if key == 0 {
		key = DefaultAdvisoryKey
	}
	return &PgAdvisory{raw: raw, key: key, interval: interval}
}

func (p *PgAdvisory) IsLeader() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.leader
}

func (p *PgAdvisory) Describe() string { return "postgres advisory lock" }

// Start dispara o loop de eleição até o ctx ser cancelado.
func (p *PgAdvisory) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(p.interval)
		defer t.Stop()
		p.tryAcquire(ctx) // tenta já no boot
		for {
			select {
			case <-ctx.Done():
				p.release()
				return
			case <-t.C:
				p.tryAcquire(ctx)
			}
		}
	}()
}

// tryAcquire mantém/adquire a liderança. Se já é líder, verifica que a conexão
// (e portanto o lock) segue viva; senão, tenta adquirir numa conexão dedicada.
func (p *PgAdvisory) tryAcquire(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.leader && p.conn != nil {
		if err := p.conn.PingContext(ctx); err != nil {
			log.Printf("[leader] lost the lock connection (%v) — giving up leadership", err)
			_ = p.conn.Close()
			p.conn = nil
			p.leader = false
		}
		return
	}

	conn, err := p.raw.Conn(ctx)
	if err != nil {
		return // sem conexão; tenta de novo no próximo tick
	}
	var got bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", p.key).Scan(&got); err != nil || !got {
		_ = conn.Close()
		return
	}
	p.conn = conn
	p.leader = true
	log.Printf("[leader] took leadership (advisory lock %d)", p.key)
}

func (p *PgAdvisory) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		_, _ = p.conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", p.key)
		_ = p.conn.Close()
		p.conn = nil
	}
	p.leader = false
}
