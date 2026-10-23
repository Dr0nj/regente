package journal

import (
	"context"
	"testing"
	"time"
)

func TestRemoteCancellationRetainsUncertainty(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{{RemoteUnknownExitCode, "uncertain"}, {UnknownExitCode, "cancel_pending"}} {
		t.Run(tc.want, func(t *testing.T) {
			j, _ := openTest(t)
			started(t, j, "remote")
			e, err := j.Entry("remote")
			if err != nil {
				t.Fatal(err)
			}
			began, release := make(chan struct{}), make(chan struct{})
			w := NewWorker(j, nil, func(context.Context, Envelope, func(string)) (int, string) {
				close(began)
				<-release
				return tc.code, "remote receipt absent"
			}, 1)
			w.launch(context.Background(), e)
			<-began
			m := message("remote")
			m.Kind = "cancel"
			m.MessageID = "remote-cancel"
			if _, err = j.Accept(m); err != nil {
				t.Fatal(err)
			}
			close(release)
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				e, err = j.Entry("remote")
				if err != nil {
					t.Fatal(err)
				}
				if e.State == tc.want {
					break
				}
				time.Sleep(time.Millisecond)
			}
			w.Stop()
			if e.State != tc.want {
				t.Fatalf("got %s want %s", e.State, tc.want)
			}
		})
	}
}
