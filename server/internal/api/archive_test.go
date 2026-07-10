package api

// ADV-5 — superfície de leitura dos archives: lista/download admin-only,
// nome de arquivo validado (nada de path traversal).

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dr0nj/regente-server/internal/db"
)

func setArchiveDir(t *testing.T, d *db.DB) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := d.Exec(`INSERT INTO settings(key,value) VALUES('archive_dir',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, dir); err != nil {
		t.Fatalf("set archive_dir: %v", err)
	}
	return dir
}

func TestArchiveAPI_ListaEBaixa(t *testing.T) {
	srv, d := newOpsTestServer(t)
	c := srv.Client()
	dir := setArchiveDir(t, d)

	content := `{"id":"i-1","status":"OK"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "instances-2020-01-01.ndjson"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Lixo no diretório não vira item da lista.
	_ = os.WriteFile(filepath.Join(dir, "notas.txt"), []byte("x"), 0o644)

	resp := doReq(t, c, http.MethodGet, srv.URL+"/api/archive", "test-token", "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("list esperava 200, veio %d", resp.StatusCode)
	}
	var out struct {
		Archives []struct {
			File string `json:"file"`
			Day  string `json:"day"`
		} `json:"archives"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Archives) != 1 || out.Archives[0].Day != "2020-01-01" {
		t.Fatalf("lista errada: %+v", out.Archives)
	}

	resp = doReq(t, c, http.MethodGet, srv.URL+"/api/archive/instances-2020-01-01.ndjson", "test-token", "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("download esperava 200, veio %d", resp.StatusCode)
	}
	if b, _ := io.ReadAll(resp.Body); string(b) != content {
		t.Fatalf("conteúdo do download difere: %q", b)
	}
}

func TestArchiveAPI_NomeInvalidoE404(t *testing.T) {
	srv, d := newOpsTestServer(t)
	c := srv.Client()
	setArchiveDir(t, d)

	// Path traversal / nome fora do padrão → 400.
	for _, name := range []string{"..%2F..%2Fetc%2Fpasswd", "instances-2020-01-01.txt", "x.ndjson"} {
		resp := doReq(t, c, http.MethodGet, srv.URL+"/api/archive/"+name, "test-token", "")
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("nome %q esperava 400, veio %d", name, resp.StatusCode)
		}
	}
	// Nome válido mas inexistente → 404.
	resp := doReq(t, c, http.MethodGet, srv.URL+"/api/archive/instances-2020-01-02.ndjson", "test-token", "")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("inexistente esperava 404, veio %d", resp.StatusCode)
	}
}

func TestArchiveAPI_AdminOnly(t *testing.T) {
	srv, d := newOpsTestServer(t)
	c := srv.Client()
	op := newOperatorToken(t, d, "op", nil)
	for _, path := range []string{"/api/archive", "/api/archive/instances-2020-01-01.ndjson"} {
		resp := doReq(t, c, http.MethodGet, srv.URL+path, op, "")
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("operator em %s esperava 403, veio %d", path, resp.StatusCode)
		}
	}
}
