package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvProvider(t *testing.T) {
	t.Setenv("REGENTE_SECRET_GITHUB_TOKEN", "envtok")
	v, ok := EnvProvider{}.Get("github_token")
	if !ok || v != "envtok" {
		t.Fatalf("env get: %q %v", v, ok)
	}
	if _, ok := (EnvProvider{}).Get("missing"); ok {
		t.Fatal("missing should be not found")
	}
}

func TestFileProvider(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(p, []byte(`{"webhook_secret":"filesec"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fp := NewFileProvider(p)
	if v, ok := fp.Get("webhook_secret"); !ok || v != "filesec" {
		t.Fatalf("file get: %q %v", v, ok)
	}
	if _, ok := fp.Get("github_token"); ok {
		t.Fatal("absent key should miss")
	}
	// arquivo inexistente = inerte, sem panic.
	if _, ok := NewFileProvider(filepath.Join(t.TempDir(), "nope.json")).Get("x"); ok {
		t.Fatal("missing file should miss")
	}
}

func TestChainPrecedenceAndGetOr(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	_ = os.WriteFile(p, []byte(`{"k":"fromfile"}`), 0o600)
	t.Setenv("REGENTE_SECRET_K", "fromenv")
	c := Default(p)
	// env tem prioridade sobre arquivo.
	if v, _ := c.Get("k"); v != "fromenv" {
		t.Fatalf("precedence: got %q want fromenv", v)
	}
	if got := c.GetOr("absent", "fallback"); got != "fallback" {
		t.Fatalf("GetOr fallback: %q", got)
	}
}
