package journal

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestKubeObserverResumesOnlyRestartMarkedCurrentEffect(t *testing.T) {
	for _, state := range []string{"running", "uncertain", "failed"} {
		t.Run(state, func(t *testing.T) {
			j, path := openTest(t)
			started(t, j, "one")
			j.Close()
			reopened, err := Open(path, "worker")
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			tr := &fakeTransport{state: state, authorized: true}
			var submit, observe atomic.Int32
			w := NewWorker(reopened, tr, func(context.Context, Envelope, func(string)) (int, string) { submit.Add(1); return 0, "bad replay" }, 1)
			defer w.Stop()
			w.Resumable = func(Entry) bool { return true }
			w.Reconcile = func(context.Context, Envelope, func(string)) (int, string) {
				observe.Add(1)
				return 0, "observed original"
			}
			e, _ := reopened.Entry("one")
			if err := w.Sync(context.Background(), e); err != nil {
				t.Fatal(err)
			}
			if state == "running" {
				waitEntry(t, reopened, "one", "result_pending")
				if observe.Load() != 1 {
					t.Fatal(observe.Load())
				}
			} else if observe.Load() != 0 {
				t.Fatal("resumed unauthorized observation")
			}
			if submit.Load() != 0 {
				t.Fatal("replayed effect")
			}
		})
	}
}
func TestKubeObserverDoesNotResumeExplicitUncertainty(t *testing.T) {
	j, _ := openTest(t)
	started(t, j, "one")
	j.Uncertain("one", "remote result lost")
	w := NewWorker(j, &fakeTransport{}, nil, 1)
	w.Resumable = func(Entry) bool { return true }
	w.Reconcile = func(context.Context, Envelope, func(string)) (int, string) {
		t.Error("unexpected automatic resume")
		return 0, ""
	}
	e, _ := j.Entry("one")
	if err := w.Sync(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}
