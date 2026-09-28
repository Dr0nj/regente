package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cada negativo altera só a fixture e exige a causa específica.
func TestCheck_RejectsBrokenAndStaleArtifacts(t *testing.T) {
	for _, kind := range []string{"markdown", "reference", "image", "anchor", "stale", "missing", "extra"} {
		t.Run(kind, func(t *testing.T) {
			repo := t.TempDir()
			out := filepath.Join(repo, "docs", "site")
			writeFile(t, repo, "README.md", "# Example\n\n[guide](docs/guide.md#details)\n")
			writeFile(t, repo, "docs/guide.md", "# Guide\n\n## Details\n\nText.\n")
			if _, err := Build(repo, out); err != nil {
				t.Fatal(err)
			}
			if err := Check(repo, out); err != nil {
				t.Fatalf("baseline válida: %v", err)
			}
			want := ""
			switch kind {
			case "markdown":
				writeFile(t, repo, "README.md", "# Example\n\n[missing](missing.md)\n")
				want = "source link README.md"
			case "reference":
				writeFile(t, repo, "README.md", "# Example\n\n[missing][ref]\n\n[ref]: missing.md\n")
				want = "source link README.md"
			case "image":
				writeFile(t, repo, "README.md", "# Example\n\n<img src=\"missing.png\" />\n")
				want = "source link README.md"
			case "anchor":
				writeFile(t, repo, "README.md", "# Example\n\n[guide](docs/guide.md#absent)\n")
				want = "missing anchor"
			case "stale":
				writeFile(t, repo, "docs/site/index.html", "stale")
				want = "missing or stale: index.html"
			case "missing":
				if err := os.Remove(filepath.Join(out, "guide.html")); err != nil {
					t.Fatal(err)
				}
				want = "missing or stale: guide.html"
			case "extra":
				writeFile(t, repo, "docs/site/obsolete.html", "obsolete")
				want = "unexpected generated file: obsolete.html"
			}
			before, _ := os.ReadFile(filepath.Join(out, "index.html"))
			err := Check(repo, out)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("queria %q, veio %v", want, err)
			}
			after, _ := os.ReadFile(filepath.Join(out, "index.html"))
			if !bytes.Equal(before, after) {
				t.Fatal("check não deve reparar o site")
			}
		})
	}
}

func TestCheck_RespectsCodeExamplesAndTextLineEndings(t *testing.T) {
	repo, out := t.TempDir(), t.TempDir()
	writeFile(t, repo, "README.md", "# Example\n\n~~~md\n[historical example](absent.md)\n~~~\n\n[external](https://example.test/missing)\n")
	if _, err := Build(repo, out); err != nil {
		t.Fatal(err)
	}
	if err := Check(repo, out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if err := os.WriteFile(filepath.Join(out, "index.html"), bytes.ReplaceAll(raw, []byte("\n"), []byte("\r\n")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Check(repo, out); err != nil {
		t.Fatalf("checkout CRLF deve ser aceito: %v", err)
	}
}

// Resíduos de ensaio e estado operacional nunca são fonte documental.
func TestCheck_IgnoresRuntimeArtifacts(t *testing.T) {
	repo, out := t.TempDir(), t.TempDir()
	writeFile(t, repo, "README.md", "# Example\n")
	for _, dir := range []string{".smoke/stage", ".integration/run", "server/workspace", "server/sessions", "sessions", "deploy/demo/sessions"} {
		writeFile(t, repo, dir+"/README.md", "[runtime](absent.md)\n")
	}
	if _, err := Build(repo, out); err != nil {
		t.Fatal(err)
	}
	if err := Check(repo, out); err != nil {
		t.Fatal(err)
	}
}
