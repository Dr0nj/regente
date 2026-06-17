package main

import (
	_ "embed"
	"os"
	"strings"
	"testing"
)

// Fixture WASI compilado de testdata/echo/main.go:
//
//	GOOS=wasip1 GOARCH=wasm go build -o testdata/echo.wasm ./testdata/echo/main.go
//
//go:embed testdata/echo.wasm
var echoWasm []byte

func TestExecWASM_RunsAndCapturesOutput(t *testing.T) {
	code, out := execWASM(echoWasm, "alpha beta", "linha1", 30, nil)
	if code != 0 {
		t.Fatalf("esperava exit 0, veio %d (out=%q)", code, out)
	}
	if !strings.Contains(out, "hello from wasm") {
		t.Fatalf("stdout do módulo não capturado: %q", out)
	}
	if !strings.Contains(out, "args: alpha beta") {
		t.Fatalf("args não repassados ao módulo: %q", out)
	}
	if !strings.Contains(out, "stdin: linha1") {
		t.Fatalf("stdin não repassado ao módulo: %q", out)
	}
}

func TestExecWASM_StreamsViaEmit(t *testing.T) {
	var streamed strings.Builder
	emit := func(chunk string) { streamed.WriteString(chunk) }
	code, _ := execWASM(echoWasm, "", "", 30, emit)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(streamed.String(), "hello from wasm") {
		t.Fatalf("emit não recebeu o stdout em tempo real: %q", streamed.String())
	}
}

func TestRunWASM_MissingParam(t *testing.T) {
	code, out := runWASM(map[string]interface{}{}, 5, nil)
	if code != -1 || !strings.Contains(out, "missing") {
		t.Fatalf("esperava erro de param ausente, veio code=%d out=%q", code, out)
	}
}

func TestExecuteJob_RoutesWASM(t *testing.T) {
	// Garante que o switch de jobType conhece WASM (escreve o fixture em arquivo).
	dir := t.TempDir()
	path := dir + "/echo.wasm"
	if err := os.WriteFile(path, echoWasm, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := executeJob("WASM", map[string]interface{}{"wasmPath": path}, 30, nil)
	if code != 0 || !strings.Contains(out, "hello from wasm") {
		t.Fatalf("executeJob WASM falhou: code=%d out=%q", code, out)
	}
}
