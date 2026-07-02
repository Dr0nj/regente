package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileWatch_FileAppears(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "chegou.dat")

	// cria o arquivo depois de ~1.5s, enquanto o watch está rodando
	go func() {
		time.Sleep(1500 * time.Millisecond)
		_ = os.WriteFile(target, []byte("payload"), 0o644)
	}()

	code, out := runFileWatch(map[string]interface{}{
		"path":        target,
		"intervalSec": float64(1), // JSON entrega números como float64
	}, 10, func(string) {})
	if code != 0 {
		t.Fatalf("esperava exit 0 quando o arquivo aparece, veio %d (%s)", code, out)
	}
}

func TestFileWatch_Timeout(t *testing.T) {
	dir := t.TempDir()
	code, out := runFileWatch(map[string]interface{}{
		"path":        filepath.Join(dir, "nunca.dat"),
		"intervalSec": float64(1),
	}, 2, func(string) {})
	if code != 1 {
		t.Fatalf("esperava exit 1 no timeout, veio %d (%s)", code, out)
	}
}

func TestFileWatch_StableSize(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "crescendo.dat")

	// arquivo cresce por ~2s e depois PARA — o watch só pode dar OK depois de
	// stableSec sem mudança de tamanho.
	go func() {
		f, _ := os.Create(target)
		for i := 0; i < 4; i++ {
			_, _ = f.WriteString("chunk-")
			_ = f.Sync()
			time.Sleep(500 * time.Millisecond)
		}
		_ = f.Close()
	}()

	start := time.Now()
	code, out := runFileWatch(map[string]interface{}{
		"path":        target,
		"intervalSec": float64(1),
		"stableSec":   float64(2),
	}, 20, func(string) {})
	if code != 0 {
		t.Fatalf("esperava exit 0 com arquivo estável, veio %d (%s)", code, out)
	}
	// precisa ter esperado pelo menos o crescimento (~2s) + estabilidade (2s)
	if elapsed := time.Since(start); elapsed < 3*time.Second {
		t.Fatalf("deu OK cedo demais (%s) — não esperou a estabilidade", elapsed)
	}
}

func TestFileWatch_MissingPath(t *testing.T) {
	code, out := runFileWatch(map[string]interface{}{}, 5, func(string) {})
	if code != -1 || out != "missing 'path' param" {
		t.Fatalf("esperava erro de path obrigatório, veio %d (%s)", code, out)
	}
}
