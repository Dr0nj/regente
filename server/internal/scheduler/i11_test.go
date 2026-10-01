package scheduler

import (
	"context"
	"errors"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/leader"
	"sync"
	"testing"
	"time"
)

func i11Def(id string) domain.JobDefinition {
	return domain.JobDefinition{ID: id, Team: "test", JobType: "COMMAND", Resources: map[string]int{"pool": 1}, Params: map[string]interface{}{"command": "effect"}}
}
func i11Peer(t *testing.T, f *durableFixture) (*Scheduler, *execution.Engine, *db.DB) {
	t.Helper()
	d, err := db.Open(f.dialect, f.path)
	if err != nil {
		t.Fatal(err)
	}
	s := New(f.store, d, hub.New(), time.Hour)
	s.SetClock(businessclock.Func(func() time.Time { return f.now }))
	s.RuntimePolicy = f.s.RuntimePolicy
	e := execution.New(d, s.Now)
	s.AttachDurable(e)
	rt := NewResourceTracker()
	rt.LoadFromDB(d)
	s.AttachResources(rt)
	s.ReloadDefs()
	t.Cleanup(func() { s.Stop(); d.Close() })
	return s, e, d
}
func i11Start(t *testing.T, e *execution.Engine, id string) (execution.Attempt, error) {
	t.Helper()
	o, err := e.CreateRuntimeOrder(id + "-2026-09-30")
	if err != nil {
		return execution.Attempt{}, err
	}
	intent := "start"
	if o.Attempt > 0 {
		intent = "retry"
	}
	return e.Start(o.ID, "worker", fmt.Sprint(o.Attempt+1), intent)
}
func TestI11ConcurrentReservations(t *testing.T) {
	f := durableTest(t, i11Def("a"), i11Def("b"))
	s, e, _ := i11Peer(t, f)
	if err := f.s.DurableResourceChange("pool", 1, false); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	gate := make(chan struct{})
	results := make(chan error, 2)
	for i, engine := range []*execution.Engine{f.e, e} {
		wg.Add(1)
		go func(i int, engine *execution.Engine) {
			defer wg.Done()
			<-gate
			_, err := i11Start(t, engine, []string{"a", "b"}[i])
			results <- err
		}(i, engine)
	}
	close(gate)
	wg.Wait()
	close(results)
	passed, blocked := 0, 0
	for err := range results {
		if err == nil {
			passed++
		} else if errors.Is(err, execution.ErrCapacity) {
			blocked++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || blocked != 1 || scalar(t, f.d, "SELECT COUNT(*) FROM execution_attempts") != 1 || scalar(t, f.d, "SELECT COUNT(*) FROM execution_resource_holds WHERE execution_id!=''") != 1 {
		t.Fatal(passed, blocked)
	}
	// Alterar a quota no outro nó muda imediatamente a admissão e o snapshot.
	if err := s.DurableResourceChange("pool", 0, false); err != nil {
		t.Fatal(err)
	}
	snap, err := f.s.DurableResourceSnapshot()
	if err != nil || len(snap) != 1 || snap[0].Capacity != 0 || snap[0].Used != 1 {
		t.Fatal(snap, err)
	}
	var waitingID string
	if err = f.d.QueryRow("SELECT id FROM instances WHERE status='WAITING'").Scan(&waitingID); err != nil {
		t.Fatal(err)
	}
	f.s.resources = nil // Explain durável não depende da existência do cache.
	ex, err := f.s.Explain(waitingID)
	if err != nil || len(ex.Blockers) != 1 || ex.Blockers[0].Kind != GateResource || ex.Blockers[0].Capacity != 0 || ex.Blockers[0].Used != 1 {
		t.Fatal(ex, err)
	}
	if err = s.DurableResourceChange("pool", 0, true); !errors.Is(err, execution.ErrConflict) {
		t.Fatal(err)
	}
}
func TestI11RetryOwnershipAndFollowerReceipt(t *testing.T) {
	def := i11Def("job")
	def.Retries = 1
	f := durableTest(t, def)
	_, peer, _ := i11Peer(t, f)
	first := f.running(t, "job-2026-09-30")
	result := execution.Result{Identity: executionIdentity(first), ExitCode: 1, Output: "failure confirmed"}
	if _, err := peer.Complete(result); err != nil {
		t.Fatal(err)
	}
	var owner string
	f.d.QueryRow("SELECT execution_id FROM execution_resource_holds").Scan(&owner)
	if owner != first.ExecutionID {
		t.Fatal(owner)
	}
	second, err := i11Start(t, f.e, "job")
	if err != nil {
		t.Fatal(err)
	}
	f.d.QueryRow("SELECT execution_id FROM execution_resource_holds").Scan(&owner)
	if owner != second.ExecutionID || owner == first.ExecutionID {
		t.Fatal(owner)
	}
	if _, err = peer.Complete(result); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_resource_holds WHERE execution_id=?", second.ExecutionID) != 1 {
		t.Fatal("recibo antigo liberou reserva nova")
	}
	msg, err := peer.Claim("worker")
	if err != nil || msg == nil {
		t.Fatal(msg, err)
	}
	for _, phase := range []string{"accepted", "started"} {
		if _, err = peer.Acknowledge(executionIdentity(second), phase); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = peer.Complete(execution.Result{Identity: executionIdentity(second)}); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_resource_holds") != 0 {
		t.Fatal("reserva final não liberada")
	}
}
func TestI11UnknownRetainsCapacity(t *testing.T) {
	f := durableTest(t, i11Def("a"), i11Def("b"))
	a := f.running(t, "a-2026-09-30")
	f.now = f.now.Add(6 * time.Minute)
	if _, err := f.e.Reconcile(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.CancelFor(a.OrderID, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := i11Start(t, f.e, "b"); !errors.Is(err, execution.ErrCapacity) {
		t.Fatal(err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_resource_holds") != 1 {
		t.Fatal("lease/cancel liberou efeito possivelmente ativo")
	}
	if _, err := f.e.Resolve(a.ExecutionID, a.Fence, "resolve", "operator", "cancelled", "process stopped; checked destination", true); err != nil {
		t.Fatal(err)
	}
	if _, err := i11Start(t, f.e, "b"); err != nil {
		t.Fatal(err)
	}
}
func TestI11ReconciliationAndStorageFailure(t *testing.T) {
	f := durableTest(t, i11Def("job"))
	a := f.running(t, "job-2026-09-30")
	f.d.Exec("DELETE FROM execution_resource_holds")
	if n, err := f.s.rebuildDurableResources(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_resource_holds WHERE execution_id=?", a.ExecutionID) != 1 {
		t.Fatal("reconciliação não recuperou reserva")
	}
	f.d.Exec("UPDATE execution_resource_holds SET execution_id='corrupt'")
	if _, err := f.s.rebuildDurableResources(); !errors.Is(err, execution.ErrConflict) {
		t.Fatal(err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_resource_holds") != 1 {
		t.Fatal("reserva divergente descartada")
	}
	f.d.Close()
	if _, err := f.s.rebuildDurableResources(); err == nil {
		t.Fatal("falha DB não bloqueou")
	}
}
func TestI11AgentCapacityAndOffline(t *testing.T) {
	f := durableTest(t, i11Def("a"), i11Def("b"))
	if err := f.s.ReportAgentCapacity("worker", 1, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := i11Start(t, f.e, "a"); err != nil {
		t.Fatal(err)
	}
	f.s.DurableResourceChange("pool", 10, false)
	if _, err := i11Start(t, f.e, "b"); !errors.Is(err, execution.ErrCapacity) {
		t.Fatal(err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_attempts") != 1 {
		t.Fatal("excedeu slots do agente")
	}
	ex, err := f.s.Explain("b-2026-09-30")
	if err != nil || len(ex.Blockers) != 1 || ex.Blockers[0].Kind != GateAgent || ex.Blockers[0].Detail != "no eligible agent with matching identity, capabilities and available admission capacity" {
		t.Fatal(ex, err)
	}
	f.d.Exec("UPDATE execution_agent_capacity SET last_seen=0")
	if _, err := f.s.durableAgent(i11Def("b")); !errors.Is(err, execution.ErrNotFound) {
		t.Fatal(err)
	}
	f.s.ReportAgentCapacity("worker", 4, 128, false)
	if _, err := f.s.durableAgent(i11Def("b")); !errors.Is(err, execution.ErrNotFound) {
		t.Fatal(err)
	}
	if scalar(t, f.d, "SELECT COUNT(*) FROM execution_resource_holds") != 1 {
		t.Fatal("desconexão liberou reserva")
	}
}
func TestI11FIFOEligible(t *testing.T) {
	f := durableTest(t, i11Def("z-old"), i11Def("a-new"))
	f.s.ReportAgentCapacity("worker", 1, 1, true)
	f.d.Exec("UPDATE instances SET scheduled_at=?", f.now)
	f.d.Exec("UPDATE instances SET scheduled_at=? WHERE definition_id='z-old'", f.now.Add(-time.Hour))
	f.s.tickOnce()
	if instanceState(t, f, "z-old-2026-09-30") != "RUNNING" || instanceState(t, f, "a-new-2026-09-30") != "WAITING" {
		old, _ := f.s.Explain("z-old-2026-09-30")
		newer, _ := f.s.Explain("a-new-2026-09-30")
		t.Fatal("fila elegível não preservou antiguidade", instanceState(t, f, "z-old-2026-09-30"), instanceState(t, f, "a-new-2026-09-30"), old, newer)
	}
}
func TestI11PostgresContracts(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*testing.T)
	}{{"concurrent-reservations", TestI11ConcurrentReservations}, {"follower-receipt", TestI11RetryOwnershipAndFollowerReceipt}, {"unknown-capacity", TestI11UnknownRetainsCapacity}, {"reconciliation", TestI11ReconciliationAndStorageFailure}, {"agent-capacity", TestI11AgentCapacityAndOffline}, {"fifo", TestI11FIFOEligible}} {
		t.Run(tc.name, tc.run)
	}
}

func i11Eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condição HA não ocorreu")
}
func TestI11PostgresLeadershipFencing(t *testing.T) {
	f := durableTest(t, i11Def("a"), i11Def("b"))
	s, peer, d := i11Peer(t, f)
	key := time.Now().UnixNano()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := leader.NewPgAdvisory(f.d.Raw(), key, time.Hour)
	first.Start(ctx)
	defer first.Close()
	i11Eventually(t, first.IsLeader)
	second := leader.NewPgAdvisory(d.Raw(), key, 40*time.Millisecond)
	second.Start(ctx)
	defer second.Close()
	i11Eventually(t, first.IsLeader)
	f.s.AttachLeader(first)
	s.AttachLeader(second)
	a := f.running(t, "a-2026-09-30")
	if _, err := i11Start(t, peer, "b"); err == nil {
		t.Fatal("follower admitiu trabalho")
	}
	var pid int
	var oldEpoch int64
	if err := d.QueryRow("SELECT backend_pid,epoch FROM scheduler_leadership WHERE lock_key=?", key).Scan(&pid, &oldEpoch); err != nil {
		t.Fatal(err)
	}
	// O termo antigo continua marcado true no processo: não é suficiente consultar booleano.
	if _, err := d.Exec("SELECT pg_terminate_backend(?)", pid); err != nil {
		t.Fatal(err)
	}
	i11Eventually(t, second.IsLeader)
	if !first.IsLeader() {
		t.Fatal("teste não exercitou indicador obsoleto")
	}
	if _, err := i11Start(t, f.e, "b"); err == nil {
		t.Fatal("líder obsoleto admitiu trabalho")
	}
	if _, _, err := f.s.MaterializeDaily("2026-10-01"); err == nil {
		t.Fatal("líder obsoleto materializou daily")
	}
	if _, err := s.rebuildDurableResources(); err != nil {
		t.Fatal(err)
	}
	if scalar(t, d, "SELECT COUNT(*) FROM execution_resource_holds") != 1 {
		t.Fatal("sucessão perdeu reserva")
	}
	if _, err := peer.Complete(execution.Result{Identity: executionIdentity(a)}); err != nil {
		t.Fatal(err)
	}
	if _, err := i11Start(t, peer, "b"); err != nil {
		t.Fatal(err)
	}
	var epoch int64
	d.QueryRow("SELECT epoch FROM scheduler_leadership WHERE lock_key=?", key).Scan(&epoch)
	if epoch <= oldEpoch {
		t.Fatal("termo não avançou")
	}
}

func TestI11PostgresTermWaitsForTransaction(t *testing.T) {
	f := durableTest(t, i11Def("job"))
	_, _, d := i11Peer(t, f)
	key := time.Now().UnixNano()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := leader.NewPgAdvisory(f.d.Raw(), key, time.Hour)
	first.Start(ctx)
	defer first.Close()
	i11Eventually(t, first.IsLeader)
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = first.Guard(tx); err != nil {
		t.Fatal(err)
	}
	var pid int
	d.QueryRow("SELECT backend_pid FROM scheduler_leadership WHERE lock_key=?", key).Scan(&pid)
	if _, err = d.Exec("SELECT pg_terminate_backend(?)", pid); err != nil {
		t.Fatal(err)
	}
	second := leader.NewPgAdvisory(d.Raw(), key, 40*time.Millisecond)
	second.Start(ctx)
	defer second.Close()
	i11Eventually(t, func() bool {
		return scalar(t, d, "SELECT COUNT(*) FROM pg_locks WHERE locktype='advisory' AND pid!=? AND classid=?::oid AND objid=?::oid AND objsubid=1 AND granted", pid, int64(uint64(key)>>32), int64(uint64(key)&0xffffffff)) == 1
	})
	if second.IsLeader() {
		t.Fatal("novo termo publicado antes do commit anterior")
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	i11Eventually(t, second.IsLeader)
}
