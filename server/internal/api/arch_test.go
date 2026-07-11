// ARCH-4/5 — testes do transporte SSE e do gatilho de daily dedicado.
package api

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// TestSchedulerDaily_WriterGated — ARCH-5: a rota existe, exige writer e é
// idempotente (200 com o token legado = admin; 401 sem token).
func TestSchedulerDaily_WriterGated(t *testing.T) {
	srv, _ := newOpsTestServer(t)

	// Sem token → 401.
	resp, err := srv.Client().Post(srv.URL+"/api/scheduler/daily", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sem token esperava 401, veio %d", resp.StatusCode)
	}

	// Com o token legado (admin) → 200, idempotente.
	req, _ := http.NewRequest("POST", srv.URL+"/api/scheduler/daily", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	for i := 0; i < 2; i++ {
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("chamada %d: esperava 200, veio %d", i, resp.StatusCode)
		}
	}
}

// TestAgentSSE_StreamsDispatch — ARCH-4: o stream SSE registra o agente no hub
// (via broker) e entrega um dispatch empurrado no canal Send como frame `data:`.
func TestAgentSSE_StreamsDispatch(t *testing.T) {
	d := newTestDB(t)
	t.Cleanup(func() { _ = d.Close() })
	h := hub.New()
	store := storage.NewFileStore(t.TempDir(), false)
	sched := scheduler.New(store, d, h, time.Hour)
	t.Cleanup(sched.Stop)
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Token: "test-token", Scheduler: sched, Store: store}))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest("GET", srv.URL+"/api/agent/events?id=sse-1&caps=COMMAND", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, esperava text/event-stream", ct)
	}

	// O handler registra o agente no hub via broker.touch — espera aparecer.
	var client *hub.Client
	for i := 0; i < 200 && client == nil; i++ {
		client = h.PickAgent("COMMAND", "")
		if client == nil {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if client == nil {
		t.Fatal("agente não registrou no hub pelo stream SSE")
	}

	// Empurra um dispatch; deve sair como frame data: no stream.
	client.Send <- []byte(`{"event":"dispatch","instanceId":"i-1","jobType":"COMMAND"}`)

	// Lê o body num goroutine pra poder dar timeout.
	dataCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "data:"); ok {
				dataCh <- strings.TrimSpace(v)
				return
			}
		}
		dataCh <- ""
	}()

	select {
	case got := <-dataCh:
		if !strings.Contains(got, `"instanceId":"i-1"`) {
			t.Fatalf("frame SSE não trouxe o dispatch: %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout esperando o frame SSE")
	}
}

// TestAgentSSE_AuthGate — sem token → 401.
func TestAgentSSE_AuthGate(t *testing.T) {
	srv, _ := newOpsTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/agent/events?id=x&caps=COMMAND")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sem token esperava 401, veio %d", resp.StatusCode)
	}
}
