package telemetry

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"time"
)

// ValidateProfilingAddress não resolve DNS: diagnóstico nunca escuta fora do
// loopback numérico. Vazio mantém o listener completamente desligado.
func ValidateProfilingAddress(addr string) error {
	if addr == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("profiling address must be numeric loopback:port")
	}
	ip := net.ParseIP(host)
	p, e := strconv.Atoi(port)
	if ip == nil || !ip.IsLoopback() || e != nil || p < 0 || p > 65535 {
		return fmt.Errorf("profiling address must be numeric loopback:port")
	}
	return nil
}

// StartProfiling usa mux exclusivo; nenhuma rota é instalada no router público.
// Perfis podem conter dados privados. Acesso administrativo via loopback/SSH.
func StartProfiling(ctx context.Context, addr string) (*http.Server, net.Addr, error) {
	if err := ValidateProfilingAddress(addr); err != nil {
		return nil, nil, err
	}
	if addr == "" {
		return nil, nil, nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	mux := http.NewServeMux()
	for _, name := range []string{"heap", "allocs", "goroutine", "threadcreate", "block", "mutex"} {
		mux.Handle("/debug/pprof/"+name, pprof.Handler(name))
	}
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 125 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		if raw := q.Get("seconds"); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > 60 {
				http.Error(w, "seconds must be between 1 and 60", 400)
				return
			}
		}
		if r.URL.Path == "/debug/pprof/profile" && q.Get("seconds") == "" {
			q.Set("seconds", "30")
		}
		if r.URL.Path == "/debug/pprof/trace" && q.Get("seconds") == "" {
			q.Set("seconds", "1")
		}
		r.URL.RawQuery = q.Encode()
		mux.ServeHTTP(w, r)
	})
	go func() { _ = srv.Serve(ln) }()
	go func() { <-ctx.Done(); _ = srv.Close() }()
	return srv, ln.Addr(), nil
}
