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
// I11: decisões duráveis validam o termo e o lock real na própria transação.
// O claim legada continua restrito ao perfil development; não prova fencing.
package leader

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"github.com/Dr0nj/regente-server/internal/db"
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

	op     sync.Mutex
	mu     sync.RWMutex
	leader bool
	closed bool
	epoch  int64
	pid    int
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
func (p *PgAdvisory) tryAcquire(parent context.Context) {
	p.op.Lock()
	defer p.op.Unlock()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	p.mu.RLock()
	closed, active, held := p.closed, p.leader, p.conn
	p.mu.RUnlock()
	if closed {
		return
	}
	if active && held != nil {
		if err := held.PingContext(ctx); err != nil {
			discard(held)
			p.mu.Lock()
			p.conn = nil
			p.leader = false
			p.mu.Unlock()
			log.Printf("[leader] session lost: %v", err)
		}
		return
	}
	conn, err := p.raw.Conn(ctx)
	if err != nil {
		return
	}
	var got bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", p.key).Scan(&got); err != nil {
		discard(conn)
		return
	}
	if !got {
		_ = conn.Close()
		return
	}
	if _, err = conn.ExecContext(ctx, "INSERT INTO scheduler_leadership(lock_key,epoch,backend_pid) VALUES($1,0,0) ON CONFLICT(lock_key) DO NOTHING", p.key); err != nil {
		discard(conn)
		return
	}
	var epoch int64
	var pid int
	if err = conn.QueryRowContext(ctx, "UPDATE scheduler_leadership SET epoch=epoch+1,backend_pid=pg_backend_pid() WHERE lock_key=$1 RETURNING epoch,backend_pid", p.key).Scan(&epoch, &pid); err != nil {
		discard(conn)
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		discard(conn)
		return
	}
	p.epoch, p.pid, p.conn, p.leader = epoch, pid, conn, true
	p.mu.Unlock()
	log.Printf("[leader] took leadership (advisory lock %d, term %d)", p.key, epoch)
}
func (p *PgAdvisory) release() {
	p.op.Lock()
	defer p.op.Unlock()
	p.mu.Lock()
	conn := p.conn
	p.conn = nil
	p.leader = false
	p.mu.Unlock()
	if conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", p.key)
		cancel()
		if err != nil {
			discard(conn)
		} else {
			_ = conn.Close()
		}
	}
}

// Close encerra a sessão líder; não devolve ao pool uma conexão ainda com lock.
func (p *PgAdvisory) Close() { p.mu.Lock(); p.closed = true; p.mu.Unlock(); p.release() }
func discard(c *sql.Conn)    { _ = c.Raw(func(any) error { return driver.ErrBadConn }); _ = c.Close() }

var ErrNotLeader = errors.New("scheduler leadership is unavailable or stale")

// Guard lineariza a decisão antes da próxima aquisição de liderança. A linha
// bloqueada impede publicar um novo termo no meio desta transação. pg_locks
// confirma que o backend ainda detém ESTA chave nesta base, sem inferir TTL.
func (p *PgAdvisory) Guard(tx *db.Tx) error {
	p.mu.RLock()
	active, epoch, pid, key := p.leader, p.epoch, p.pid, p.key
	p.mu.RUnlock()
	if !active {
		return ErrNotLeader
	}
	var stored int64
	var owner int
	if err := tx.QueryRow("SELECT epoch,backend_pid FROM scheduler_leadership WHERE lock_key=? FOR SHARE", key).Scan(&stored, &owner); err != nil {
		return err
	}
	if stored != epoch || owner != pid {
		return ErrNotLeader
	}
	var held bool
	err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND pid=? AND granted AND mode='ExclusiveLock' AND classid=?::oid AND objid=?::oid AND objsubid=1)", pid, int64(uint64(key)>>32), int64(uint64(key)&0xffffffff)).Scan(&held)
	if err != nil {
		return err
	}
	if !held {
		return ErrNotLeader
	}
	return nil
}
