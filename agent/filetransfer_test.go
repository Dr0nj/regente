package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/pkg/sftp"
)

func TestFileTransfer_LocalSingleChecksum(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "origem.csv")
	dst := filepath.Join(dir, "destino.csv")
	if err := os.WriteFile(src, []byte("a;b;c\n1;2;3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runFileTransfer(map[string]interface{}{
		"src": src, "dst": dst, "checksum": true,
	}, 30, func(string) {})
	if code != 0 {
		t.Fatalf("esperava exit 0, veio %d (%s)", code, out)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "a;b;c\n1;2;3\n" {
		t.Fatalf("destino errado: %q err=%v", got, err)
	}
	if !strings.Contains(out, "sha256✓") {
		t.Fatalf("esperava sha256✓ no output: %s", out)
	}
	// escrita atômica: não pode sobrar .part
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatalf("sobrou %s.part", dst)
	}
	// origem preservada (deleteSource default false)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("origem sumiu sem deleteSource: %v", err)
	}
}

func TestFileTransfer_GlobToDirDeleteSource(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	for i := 1; i <= 3; i++ {
		name := filepath.Join(srcDir, fmt.Sprintf("carga_%d.csv", i))
		if err := os.WriteFile(name, []byte(fmt.Sprintf("linha %d", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// arquivo fora do glob não pode ser tocado
	outro := filepath.Join(srcDir, "ignorar.txt")
	os.WriteFile(outro, []byte("x"), 0o644)

	code, out := runFileTransfer(map[string]interface{}{
		"src": filepath.Join(srcDir, "*.csv"), "dst": dstDir, "deleteSource": true,
	}, 30, func(string) {})
	if code != 0 {
		t.Fatalf("esperava exit 0, veio %d (%s)", code, out)
	}
	for i := 1; i <= 3; i++ {
		if _, err := os.Stat(filepath.Join(dstDir, fmt.Sprintf("carga_%d.csv", i))); err != nil {
			t.Fatalf("faltou carga_%d.csv no destino: %v", i, err)
		}
		if _, err := os.Stat(filepath.Join(srcDir, fmt.Sprintf("carga_%d.csv", i))); !os.IsNotExist(err) {
			t.Fatalf("deleteSource não removeu carga_%d.csv", i)
		}
	}
	if _, err := os.Stat(outro); err != nil {
		t.Fatalf("mexeu em arquivo fora do glob: %v", err)
	}
}

func TestFileTransfer_OverwriteFalse(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "novo.dat")
	dst := filepath.Join(dir, "existente.dat")
	os.WriteFile(src, []byte("novo"), 0o644)
	os.WriteFile(dst, []byte("antigo"), 0o644)

	code, out := runFileTransfer(map[string]interface{}{
		"src": src, "dst": dst, "overwrite": false,
	}, 30, func(string) {})
	if code != 1 || !strings.Contains(out, "overwrite") {
		t.Fatalf("esperava exit 1 com aviso de overwrite, veio %d (%s)", code, out)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "antigo" {
		t.Fatalf("destino foi sobrescrito com overwrite=false: %q", got)
	}
}

func TestFileTransfer_MultiExigeDiretorio(t *testing.T) {
	srcDir := t.TempDir()
	for i := 1; i <= 2; i++ {
		os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("f%d.txt", i)), []byte("x"), 0o644)
	}
	code, out := runFileTransfer(map[string]interface{}{
		"src": filepath.Join(srcDir, "*.txt"),
		"dst": filepath.Join(t.TempDir(), "arquivo-unico.txt"),
	}, 30, func(string) {})
	if code != 1 || !strings.Contains(out, "directory") {
		t.Fatalf("esperava exit 1 exigindo diretório, veio %d (%s)", code, out)
	}
}

func TestFileTransfer_ParamsFaltando(t *testing.T) {
	if code, _ := runFileTransfer(map[string]interface{}{"dst": "/x"}, 5, func(string) {}); code != -1 {
		t.Fatalf("sem src: esperava -1, veio %d", code)
	}
	if code, _ := runFileTransfer(map[string]interface{}{"src": "/x"}, 5, func(string) {}); code != -1 {
		t.Fatalf("sem dst: esperava -1, veio %d", code)
	}
}

func TestFileTransfer_OrigemInexistente(t *testing.T) {
	code, out := runFileTransfer(map[string]interface{}{
		"src": filepath.Join(t.TempDir(), "nao-existe.dat"),
		"dst": filepath.Join(t.TempDir(), "x.dat"),
	}, 5, func(string) {})
	if code != 1 || !strings.Contains(out, "source has no files") {
		t.Fatalf("esperava exit 1 origem sem arquivos, veio %d (%s)", code, out)
	}
}

// sftp in-process: servidor do pkg/sftp servindo o filesystem real sobre um
// net.Pipe — testa o backend sftpFS de verdade (Glob/Create/rename/Remove)
// sem handshake ssh. O servidor fala caminhos POSIX; no Windows local o teste
// é pulado (a CI ubuntu cobre).
func TestFileTransfer_SFTPBackend(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("servidor sftp in-process usa caminhos POSIX; coberto na CI linux")
	}
	cli, srv := net.Pipe()
	server, err := sftp.NewServer(srv)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = server.Serve() }()
	t.Cleanup(func() { server.Close(); wg.Wait() })

	client, err := sftp.NewClientPipe(cli, cli)
	if err != nil {
		t.Fatal(err)
	}
	fs := &sftpFS{c: client}
	defer fs.close()

	dir := t.TempDir()
	src := filepath.Join(dir, "remoto.dat")
	if err := os.WriteFile(src, []byte("conteudo-sftp"), 0o644); err != nil {
		t.Fatal(err)
	}

	// expand exato + glob
	files, err := fs.expand(filepath.Join(dir, "*.dat"))
	if err != nil || len(files) != 1 {
		t.Fatalf("glob sftp: files=%v err=%v", files, err)
	}

	// writeFile atômico + open de volta
	dst := filepath.Join(dir, "gravado.dat")
	r, size, err := fs.open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := fs.writeFile(dst, r, size); err != nil {
		t.Fatalf("writeFile sftp: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "conteudo-sftp" {
		t.Fatalf("conteúdo errado via sftp: %q err=%v", got, err)
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatalf("sobrou .part no sftp")
	}
	if ok, _ := fs.exists(dst); !ok {
		t.Fatal("exists(dst) devia ser true")
	}
	if err := fs.remove(dst); err != nil {
		t.Fatalf("remove sftp: %v", err)
	}
}

// fakeS3 — S3 mínimo em httptest (path-style /bucket/chave) que valida a
// presença da assinatura SigV4 e do x-amz-content-sha256 exigido pelo S3.
func fakeS3(t *testing.T) (*httptest.Server, map[string][]byte) {
	t.Helper()
	var mu sync.Mutex
	store := map[string][]byte{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Errorf("%s %s sem assinatura SigV4", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Amz-Content-Sha256") == "" {
			t.Errorf("%s %s sem x-amz-content-sha256", r.Method, r.URL.Path)
		}
		key := strings.TrimPrefix(r.URL.Path, "/")
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			store[key] = b
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			b, ok := store[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write(b)
		case http.MethodHead:
			if _, ok := store[key]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			delete(store, key)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(ts.Close)
	return ts, store
}

func TestFileTransfer_S3UploadDownload(t *testing.T) {
	ts, store := fakeS3(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "sobe.bin")
	payload := strings.Repeat("regente-mft ", 1000)
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	params := map[string]interface{}{
		"src": src, "dst": "s3://meu-bucket/entrada/sobe.bin",
		"checksum": true, "region": "us-east-1",
		"accessKeyId": "AKTEST", "secretAccessKey": "SKTEST",
		"s3Endpoint": ts.URL,
	}
	code, out := runFileTransfer(params, 30, func(string) {})
	if code != 0 {
		t.Fatalf("upload s3: esperava 0, veio %d (%s)", code, out)
	}
	if got := string(store["meu-bucket/entrada/sobe.bin"]); got != payload {
		t.Fatalf("payload no s3 divergente (%d bytes)", len(got))
	}

	// volta: s3 → local, movendo (deleteSource apaga o objeto)
	back := filepath.Join(dir, "desce.bin")
	code, out = runFileTransfer(map[string]interface{}{
		"src": "s3://meu-bucket/entrada/sobe.bin", "dst": back,
		"checksum": true, "deleteSource": true, "region": "us-east-1",
		"accessKeyId": "AKTEST", "secretAccessKey": "SKTEST",
		"s3Endpoint": ts.URL,
	}, 30, func(string) {})
	if code != 0 {
		t.Fatalf("download s3: esperava 0, veio %d (%s)", code, out)
	}
	got, err := os.ReadFile(back)
	if err != nil || string(got) != payload {
		t.Fatalf("download divergente: err=%v len=%d", err, len(got))
	}
	if _, ok := store["meu-bucket/entrada/sobe.bin"]; ok {
		t.Fatal("deleteSource não removeu o objeto s3")
	}
}

func TestFileTransfer_S3GlobRejeitado(t *testing.T) {
	ts, _ := fakeS3(t)
	code, out := runFileTransfer(map[string]interface{}{
		"src": "s3://bkt/dados/*.csv", "dst": t.TempDir() + string(os.PathSeparator),
		"region": "us-east-1", "accessKeyId": "AK", "secretAccessKey": "SK",
		"s3Endpoint": ts.URL,
	}, 10, func(string) {})
	if code != -1 || !strings.Contains(out, "glob") {
		t.Fatalf("esperava -1 rejeitando glob em s3, veio %d (%s)", code, out)
	}
}

func TestFileTransfer_S3SemCredencial(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	code, out := runFileTransfer(map[string]interface{}{
		"src": "s3://bkt/x", "dst": "/tmp/x", "region": "us-east-1",
	}, 10, func(string) {})
	if code != -1 || !strings.Contains(out, "credentials") {
		t.Fatalf("esperava -1 sem credencial, veio %d (%s)", code, out)
	}
}
