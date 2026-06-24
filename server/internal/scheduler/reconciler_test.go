package scheduler

import (
	"errors"
	"testing"
	"time"
)

// helper: monta um reconciler com fakes e devolve ponteiros pros side-effects.
func newFakeReconciler(mode string) (*DriftReconciler, *struct {
	drifted      bool
	checkErr     error
	reconcileN   int
	reconcileErr error
	alerts       []string
	leader       bool
}) {
	st := &struct {
		drifted      bool
		checkErr     error
		reconcileN   int
		reconcileErr error
		alerts       []string
		leader       bool
	}{leader: true}
	r := NewDriftReconciler(
		func() (bool, string, error) { return st.drifted, "abc1234def", st.checkErr },
		func() error { st.reconcileN++; return st.reconcileErr },
		func() bool { return st.leader },
		func(sha, detail string) { st.alerts = append(st.alerts, sha+"|"+detail) },
		mode,
		time.Second,
	)
	r.logf = func(string, ...any) {} // silêncio no teste
	return r, st
}

func TestReconciler_SkipsFollower(t *testing.T) {
	r, st := newFakeReconciler("sync")
	st.leader = false
	st.drifted = true
	if got := r.runOnce(); got != "skip-follower" {
		t.Fatalf("follower não deve reconciliar; ação=%q", got)
	}
	if st.reconcileN != 0 || len(st.alerts) != 0 {
		t.Fatalf("follower mexeu no workspace/alertou: reconcile=%d alerts=%d", st.reconcileN, len(st.alerts))
	}
}

func TestReconciler_NoDrift(t *testing.T) {
	r, st := newFakeReconciler("sync")
	st.drifted = false
	if got := r.runOnce(); got != "none" {
		t.Fatalf("sem drift a ação deveria ser none, veio %q", got)
	}
	if st.reconcileN != 0 {
		t.Fatal("sem drift não pode reconciliar")
	}
}

func TestReconciler_SyncModeReconciles(t *testing.T) {
	r, st := newFakeReconciler("sync")
	st.drifted = true
	if got := r.runOnce(); got != "reconciled" {
		t.Fatalf("modo sync com drift deveria reconciliar, veio %q", got)
	}
	if st.reconcileN != 1 {
		t.Fatalf("reconcile deveria ser chamado 1×, foi %d", st.reconcileN)
	}
	if len(st.alerts) != 0 {
		t.Fatal("sync bem-sucedido não deveria alertar")
	}
}

func TestReconciler_SyncFailureAlertsAsFallback(t *testing.T) {
	r, st := newFakeReconciler("sync")
	st.drifted = true
	st.reconcileErr = errors.New("rede caiu")
	if got := r.runOnce(); got != "reconcile-error" {
		t.Fatalf("sync que falha deveria devolver reconcile-error, veio %q", got)
	}
	if len(st.alerts) != 1 {
		t.Fatal("falha no reconcile automático deveria alertar como fallback")
	}
}

func TestReconciler_AlertModeOnlyAlerts(t *testing.T) {
	r, st := newFakeReconciler("alert")
	st.drifted = true
	if got := r.runOnce(); got != "alerted" {
		t.Fatalf("modo alert deveria alertar, veio %q", got)
	}
	if st.reconcileN != 0 {
		t.Fatal("modo alert NÃO pode tocar no workspace (sem reset)")
	}
	if len(st.alerts) != 1 {
		t.Fatalf("esperava 1 alerta, veio %d", len(st.alerts))
	}
}

func TestReconciler_CheckErrorIsTransient(t *testing.T) {
	r, st := newFakeReconciler("sync")
	st.checkErr = errors.New("fetch timeout")
	if got := r.runOnce(); got != "check-error" {
		t.Fatalf("erro no check deveria devolver check-error, veio %q", got)
	}
	if st.reconcileN != 0 || len(st.alerts) != 0 {
		t.Fatal("erro de check não deve reconciliar nem alertar (transiente)")
	}
}

// Mode inválido cai em "alert" (seguro: não auto-reseta workspace).
func TestReconciler_UnknownModeDefaultsToAlert(t *testing.T) {
	r, st := newFakeReconciler("banana")
	st.drifted = true
	if got := r.runOnce(); got != "alerted" {
		t.Fatalf("modo desconhecido deveria virar alert, veio %q", got)
	}
}
