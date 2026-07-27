package main

// ADV-7 — o gerador é 100% derivado do markdown do repo: coleta README +
// READMEs de componentes + docs/*.md, reescreve links .md→.html resolvendo o
// caminho relativo da página de origem, e copia imagens locais referenciadas.
// I18N — docs internos em pt-BR (roadmap, planos, rascunhos) NÃO vão pro site.
// O site também carrega a referência de API estática (spec injetada).

import (
	"encoding/json"
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

See the [server](server/README.md) and [operations](docs/operations.md#backup).

<img src="app/public/logo.png" alt="logo" />
`)
	writeFile(t, repo, "server/README.md", "# regente-server\n\nBack to the [monorepo](../README.md).\n")
	writeFile(t, repo, "docs/operations.md", "# Operations\n\n## Backup\n\nAn [external](https://x.dev) link stays; [mcp](mcp.md) becomes html.\n")
	writeFile(t, repo, "docs/mcp.md", "# MCP\n")
	writeFile(t, repo, "docs/roadmap.md", "# Roadmap\n") // interno: fora do site
	writeFile(t, repo, "app/public/logo.png", "png-fake")

	n, err := Build(repo, out)
	if err != nil {
		t.Fatal(err)
	}
	// 4 markdown publicados (roadmap fica de fora) + a referência de API.
	if n != 5 {
		t.Fatalf("esperava 5 páginas, veio %d", n)
	}

	index, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("index.html não gerado: %v", err)
	}
	for _, want := range []string{
		`href="server.html"`,                    // server/README.md → server.html
		`href="operations.html#backup"`,         // âncora preservada
		`src="app/public/logo.png"`,             // asset com caminho relativo à raiz
		`<title>Regente · Regente Docs</title>`, // título do <h1> cru
		`href="api.html"`,                       // referência de API na nav
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
	op, _ := os.ReadFile(filepath.Join(out, "operations.html"))
	if !strings.Contains(string(op), `href="https://x.dev"`) {
		t.Fatal("link externo não pode ser reescrito")
	}
	if !strings.Contains(string(op), `href="mcp.html"`) {
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

// Doc interno (pt-BR) não vira página nem entra na nav — e o link pra ele fica
// como está, sem virar .html quebrado.
func TestBuild_PulaDocsInternos(t *testing.T) {
	repo := t.TempDir()
	out := t.TempDir()
	writeFile(t, repo, "README.md", "# Regente\n\n[roadmap](docs/roadmap.md) é nosso.\n")
	writeFile(t, repo, "docs/roadmap.md", "# Roadmap\n")
	writeFile(t, repo, "docs/case-study.md", "# Case study PT\n")
	writeFile(t, repo, "docs/case-study.en.md", "# Case study\n")

	if _, err := Build(repo, out); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"roadmap.html", "case-study.html"} {
		if _, err := os.Stat(filepath.Join(out, gone)); err == nil {
			t.Fatalf("%s não deveria ser gerado (doc interno/pt-BR)", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "case-study.en.html")); err != nil {
		t.Fatalf("a versão EN do case study deveria ser publicada: %v", err)
	}
	index, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if strings.Contains(string(index), `href="roadmap.html"`) {
		t.Fatal("link pra doc não publicado não pode ser reescrito pra .html")
	}
}

// A referência de API tem que abrir SEM server: a spec vai injetada (fetch é
// bloqueado em file://) e o Try-it some.
func TestBuild_ReferenciaDeAPIEstatica(t *testing.T) {
	repo := t.TempDir()
	out := t.TempDir()
	writeFile(t, repo, "README.md", "# Regente\n")
	if _, err := Build(repo, out); err != nil {
		t.Fatal(err)
	}

	page, err := os.ReadFile(filepath.Join(out, "api.html"))
	if err != nil {
		t.Fatalf("api.html não gerado: %v", err)
	}
	body := string(page)
	if !strings.Contains(body, `id="openapi-inline"`) {
		t.Fatal("a spec precisa ir INJETADA (fetch não funciona em file://)")
	}
	if !strings.Contains(body, `href="index.html">← Docs`) {
		t.Fatal("faltou o link de volta pro índice do site")
	}
	// O JSON injetado tem que ser parseável e ser a spec de verdade.
	start := strings.Index(body, `id="openapi-inline" type="application/json">`)
	start += len(`id="openapi-inline" type="application/json">`)
	raw := body[start : start+strings.Index(body[start:], "</script>")]
	var spec struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]any
	}
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatalf("spec injetada não é JSON válido: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.") || len(spec.Paths) == 0 {
		t.Fatalf("spec injetada não parece o contrato: openapi=%q paths=%d", spec.OpenAPI, len(spec.Paths))
	}
	// Nada de "<" cru dentro do <script> (fecharia a tag mais cedo).
	if strings.Contains(raw, "<") {
		t.Fatal("o JSON injetado precisa escapar '<'")
	}

	for _, f := range []string{"openapi.yaml", "openapi.json"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Fatalf("%s deveria ir junto (import em Postman/codegen): %v", f, err)
		}
	}
}

func TestBuild_RepoVazioFalha(t *testing.T) {
	if _, err := Build(t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("repo sem markdown deveria falhar")
	}
}
