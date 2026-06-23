package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
)

// newTestServer monta o router real (DB in-memory + hub) com o legacy token "test-token"
// (authMiddleware aceita == admin), e devolve o httptest.Server.
func newTestServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	d := newTestDB(t)
	router := NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token"})
	srv := httptest.NewServer(router)
	return srv, func() { srv.Close(); d.Close() }
}

func authReq(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	var r *http.Request
	var err error
	if body != "" {
		r, err = http.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer test-token")
	return r
}

// E2E — exercita o servidor real ponta-a-ponta por HTTP: health, auth gating, métricas,
// e round-trip de settings (config durável).
func TestE2E_ServerFlow(t *testing.T) {
	srv, done := newTestServer(t)
	defer done()
	c := srv.Client()

	// 1) /readyz público → 200 ready.
	if resp, err := c.Get(srv.URL + "/readyz"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("/readyz esperava 200, veio %v err=%v", statusOf(resp), err)
	}

	// 2) /metrics público → 200 com formato Prometheus.
	resp, err := c.Get(srv.URL + "/metrics")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("/metrics esperava 200, veio %v err=%v", statusOf(resp), err)
	}
	if b := readBody(resp); !strings.Contains(b, "regente_up 1") {
		t.Fatalf("/metrics sem regente_up: %.120s", b)
	}

	// 3) Login com credencial errada → 401.
	if resp, _ := c.Post(srv.URL+"/api/auth/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"nope"}`)); resp.StatusCode != 401 {
		t.Fatalf("login errado esperava 401, veio %d", resp.StatusCode)
	}

	// 4) Endpoint protegido SEM token → 401.
	if resp, _ := c.Get(srv.URL + "/api/users"); resp.StatusCode != 401 {
		t.Fatalf("/api/users sem token esperava 401, veio %d", resp.StatusCode)
	}

	// 5) Endpoint protegido COM legacy token → 200.
	if resp, err := c.Do(authReq(t, http.MethodGet, srv.URL+"/api/users", "")); err != nil || resp.StatusCode != 200 {
		t.Fatalf("/api/users com token esperava 200, veio %v err=%v", statusOf(resp), err)
	}

	// 6) Settings round-trip: PUT env_label → GET /api/env reflete (config durável).
	if resp, err := c.Do(authReq(t, http.MethodPut, srv.URL+"/api/settings", `{"env_label":"E2E"}`)); err != nil || resp.StatusCode != 200 {
		t.Fatalf("PUT settings esperava 200, veio %v err=%v", statusOf(resp), err)
	}
	if b := readBody(mustGet(t, c, srv.URL+"/api/env")); !strings.Contains(b, `"E2E"`) {
		t.Fatalf("/api/env não refletiu o settings: %s", b)
	}
}

// Carga leve: dispara N requests concorrentes a /readyz e exige 100% de sucesso.
// Prova que o servidor aguenta concorrência sem erros nem panics (smoke de carga).
func TestLoad_ReadyzConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de carga em -short")
	}
	srv, done := newTestServer(t)
	defer done()
	c := srv.Client()

	const workers, perWorker = 40, 50 // 2000 requests
	var ok, fail int64
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				resp, err := c.Get(srv.URL + "/readyz")
				if err != nil || resp.StatusCode != 200 {
					atomic.AddInt64(&fail, 1)
					continue
				}
				resp.Body.Close()
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	wg.Wait()
	total := workers * perWorker
	rps := float64(total) / time.Since(start).Seconds()
	t.Logf("carga: %d reqs, %d ok, %d falhas, %.0f req/s", total, ok, fail, rps)
	if fail != 0 {
		t.Fatalf("teste de carga teve %d falhas (esperava 0)", fail)
	}
}

// --- helpers ---

func statusOf(r *http.Response) any {
	if r == nil {
		return "nil-response"
	}
	return r.StatusCode
}

func readBody(r *http.Response) string {
	if r == nil {
		return ""
	}
	defer r.Body.Close()
	buf := make([]byte, 4096)
	n, _ := r.Body.Read(buf)
	return string(buf[:n])
}

func mustGet(t *testing.T, c *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}
