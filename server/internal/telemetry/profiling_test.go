package telemetry

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestProfilingLoopbackAndLifecycle(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:6060", "[::]:6060", "localhost:6060", "192.0.2.1:6060", "127.0.0.1:99999", "127.0.0.1"} {
		if ValidateProfilingAddress(addr) == nil {
			t.Fatalf("aceitou %q", addr)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if s, a, e := StartProfiling(ctx, ""); s != nil || a != nil || e != nil {
		t.Fatal("listener default não desligado")
	}
	s, a, err := StartProfiling(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/health", 404}, {"/debug/pprof/goroutine?debug=1", 200},
		{"/debug/pprof/profile?seconds=61", 400}, {"/debug/pprof/profile?seconds=-1", 400},
		{"/debug/pprof/trace?seconds=no", 400},
	} {
		r, e := client.Get("http://" + a.String() + tc.path)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != tc.code {
			t.Fatalf("%s: %d", tc.path, r.StatusCode)
		}
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r, e := client.Get("http://" + a.String() + "/debug/pprof/heap")
		if e != nil {
			return
		}
		r.Body.Close()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("listener não encerrou com contexto")
}
