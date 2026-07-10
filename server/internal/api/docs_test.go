package api

// ADV-7 — hosting do site de docs em /docs: arquivo direto, index na raiz,
// 404 sem fallback de SPA, path traversal contido.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

func TestDocsDir_ServeEstatico(t *testing.T) {
	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "index.html"), []byte("<h1>Regente Docs</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "operacao.html"), []byte("<h1>Op</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := newTestDB(t)
	t.Cleanup(func() { _ = d.Close() })
	store := storage.NewFileStore(t.TempDir(), false)
	sched := scheduler.New(store, d, hub.New(), time.Hour)
	t.Cleanup(sched.Stop)
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token", Scheduler: sched, Store: store, DocsDir: docs}))
	t.Cleanup(srv.Close)

	get := func(path string) (int, string) {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	if code, body := get("/docs/"); code != 200 || body != "<h1>Regente Docs</h1>" {
		t.Fatalf("/docs/ esperava o index, veio %d %q", code, body)
	}
	if code, body := get("/docs/operacao.html"); code != 200 || body != "<h1>Op</h1>" {
		t.Fatalf("/docs/operacao.html esperava a página, veio %d %q", code, body)
	}
	// /docs sem barra redireciona (o client segue) → index.
	if code, body := get("/docs"); code != 200 || body != "<h1>Regente Docs</h1>" {
		t.Fatalf("/docs esperava redirect→index, veio %d %q", code, body)
	}
	if code, _ := get("/docs/nao-existe.html"); code != 404 {
		t.Fatalf("página inexistente esperava 404, veio %d", code)
	}
	// Traversal contido pelo path.Clean.
	if code, _ := get("/docs/..%2F..%2Fetc%2Fpasswd"); code != 404 {
		t.Fatalf("traversal esperava 404, veio %d", code)
	}
}

func TestDocsDir_VazioNaoRegistraRota(t *testing.T) {
	srv, _ := newOpsTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/docs/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("sem -docs-dir /docs deveria ser 404, veio %d", resp.StatusCode)
	}
}
