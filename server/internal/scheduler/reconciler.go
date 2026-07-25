package scheduler

import (
	"context"
	"log"
	"time"
)

// DriftReconciler — Enterprise/Operação: fecha o loop GitOps. O repositório tem o
// estado DESEJADO (as definitions); o runtime carrega uma cópia local do workspace.
// Quando o remoto avança (push, merge de PR) o runtime fica DESATUALIZADO = drift.
// A daily já faz `dailySync` 1×/dia, mas ENTRE dailies o drift fica invisível até
// alguém olhar a UI. Este reconciler verifica o drift periodicamente e, conforme o
// modo, RECONCILIA (fetch+reset+reload → runtime volta a bater com o Git) ou só
// ALERTA (pelos mesmos canais do R7/selfmon).
//
// Só o LÍDER reconcilia (evita N nós brigando pelo mesmo workspace) — mesmo seam
// do selfmon/scheduler. O núcleo (`runOnce`) é determinístico e devolve a ação
// tomada, então é testável sem git real (fakes injetados), como o `cpState.eval`.
type DriftReconciler struct {
	// checkDrift faz fetch leve + compara HEAD local vs origin → (drifted, remoteSHA, err).
	checkDrift func() (bool, string, error)
	// reconcile aplica o estado desejado (fetch+reset + reload das defs). Modo "sync".
	reconcile func() error
	// isLeader — só o líder reconcilia. nil = sempre líder (nó único).
	isLeader func() bool
	// onDrift — hook de alerta (modo "alert", ou quando um sync automático falha).
	onDrift func(remoteSHA, detail string)

	mode     string // "sync" = auto-reconcilia · "alert" = só avisa
	interval time.Duration
	logf     func(string, ...any)
}

// NewDriftReconciler monta o reconciler. mode != "sync" cai em "alert".
func NewDriftReconciler(
	checkDrift func() (bool, string, error),
	reconcile func() error,
	isLeader func() bool,
	onDrift func(remoteSHA, detail string),
	mode string,
	interval time.Duration,
) *DriftReconciler {
	if mode != "sync" {
		mode = "alert"
	}
	return &DriftReconciler{
		checkDrift: checkDrift, reconcile: reconcile, isLeader: isLeader,
		onDrift: onDrift, mode: mode, interval: interval, logf: log.Printf,
	}
}

// runOnce executa UMA verificação e devolve a ação tomada (para teste/observação):
// "skip-follower" · "none" · "reconciled" · "alerted" · "check-error" · "reconcile-error".
func (r *DriftReconciler) runOnce() string {
	if r.isLeader != nil && !r.isLeader() {
		return "skip-follower" // só o líder mexe no workspace
	}
	drifted, sha, err := r.checkDrift()
	if err != nil {
		r.logf("[reconciler] drift check failed (transient): %v", err)
		return "check-error"
	}
	if !drifted {
		return "none"
	}
	if r.mode == "sync" {
		if err := r.reconcile(); err != nil {
			r.logf("[reconciler] reconcile to %s failed: %v", short(sha), err)
			if r.onDrift != nil { // não conseguiu reconciliar → alerta como fallback
				r.onDrift(sha, "automatic reconcile failed: "+err.Error())
			}
			return "reconcile-error"
		}
		r.logf("[reconciler] drift reconciled → workspace synced at %s", short(sha))
		return "reconciled"
	}
	// modo "alert" — não toca no workspace, só avisa o operador.
	if r.onDrift != nil {
		r.onDrift(sha, "runtime behind Git (remote at "+short(sha)+")")
	}
	return "alerted"
}

// Start dispara o loop em background até o ctx encerrar. interval <= 0 = desligado.
// Recupera panics para nunca derrubar o processo (mesma postura do R2).
func (r *DriftReconciler) Start(ctx context.Context) {
	if r.interval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(r.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				func() {
					defer func() {
						if rec := recover(); rec != nil {
							r.logf("[reconciler] PANIC recuperado: %v", rec)
						}
					}()
					r.runOnce()
				}()
			}
		}
	}()
}
