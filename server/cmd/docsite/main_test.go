package main

// ADV-7 — o gerador é 100% derivado do markdown do repo: coleta README +
// READMEs de componentes + docs/*.md, reescreve links .md→.html resolvendo o
// caminho relativo da página de origem, e copia imagens locais referenciadas.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuild_GeraSiteCompleto(t *testing.T) {
	repo := t.TempDir()
	out := t.TempDir()
	writeFile(t, repo, "README.md", `<h1>Regente</h1>

Veja o [server](server/README.md) e a [operação](docs/operacao.md#backup).

<img src="app/public/logo.png" alt="logo" />
`)
	writeFile(t, repo, "server/README.md", "# regente-server\n\nVolta pro [monorepo](../README.md).\n")
	writeFile(t, repo, "docs/operacao.md", "# Operação\n\n## Backup\n\nLink [externo](https://x.dev) fica intacto; [roadmap](roadmap.md) vira html.\n")
	writeFile(t, repo, "docs/roadmap.md", "# Roadmap\n")
	writeFile(t, repo, "app/public/logo.png", "png-fake")

	n, err := Build(repo, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("esperava 4 páginas, veio %d", n)
	}

	index, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("index.html não gerado: %v", err)
	}
	for _, want := range []string{
		`href="server.html"`,               // server/README.md → server.html
		`href="operacao.html#backup"`,      // âncora preservada
		`src="app/public/logo.png"`,        // asset com caminho relativo à raiz
		`<title>Regente · Regente Docs</title>`, // título do <h1> cru
	} {
		if !strings.Contains(string(index), want) {
			t.Fatalf("index.html sem %q", want)
		}
	}
	// Asset copiado pro site.
	if _, err := os.Stat(filepath.Join(out, "app", "public", "logo.png")); err != nil {
		t.Fatalf("imagem referenciada não copiada: %v", err)
	}

	srv, _ := os.ReadFile(filepath.Join(out, "server.html"))
	if !strings.Contains(string(srv), `href="index.html"`) {
		t.Fatal("../README.md de uma subpágina deveria resolver pra index.html")
	}
	op, _ := os.ReadFile(filepath.Join(out, "operacao.html"))
	if !strings.Contains(string(op), `href="https://x.dev"`) {
		t.Fatal("link externo não pode ser reescrito")
	}
	if !strings.Contains(string(op), `href="roadmap.html"`) {
		t.Fatal("link entre docs/*.md deveria virar .html")
	}
	// Navegação presente em todas com a página ativa marcada.
	if !strings.Contains(string(op), `class="active"`) {
		t.Fatal("nav sem página ativa")
	}
	// Self-contained: nenhuma referência http externa no CSS/HTML estrutural.
	if strings.Contains(siteCSS, "http") {
		t.Fatal("CSS não pode puxar nada externo (zero CDN)")
	}
}

func TestBuild_RepoVazioFalha(t *testing.T) {
	if _, err := Build(t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("repo sem markdown deveria falhar")
	}
}
