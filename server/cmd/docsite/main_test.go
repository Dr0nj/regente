package main

// ADV-7 — o gerador é 100% derivado do markdown do repo: coleta README +
// READMEs de componentes + docs/*.md, reescreve links .md→.html resolvendo o
// caminho relativo da página de origem, e copia imagens locais referenciadas.
// I18N — docs internos em pt-BR (roadmap, planos, rascunhos) NÃO vão pro site.
// O site também carrega a referência de API estática (spec injetada).

import (
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"regexp"
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
	if !strings.Contains(string(index), `href="https://github.com/Dr0nj/regente/blob/main/docs/roadmap.md"`) {
		t.Fatalf("link pro roadmap devia ir pro GitHub (relativo é 404 no site), veio:\n%s", index)
	}
}

// INVARIANTE: nenhuma href relativa do site pode apontar pra fora do site. O
// gerador publica um subconjunto do markdown do repo; todo alvo de fora (script,
// diretório, README de subpasta, doc interno) tem de virar URL absoluta do
// GitHub. Sem esta trava voltam os 404 silenciosos — 6 das 13 páginas tinham,
// a index sozinha tinha 8.
func TestBuild_NenhumaHrefRelativaQuebrada(t *testing.T) {
	repo := t.TempDir()
	out := t.TempDir()
	writeFile(t, repo, "README.md", `# Regente

Install with [install.sh](install.sh), read [deploy/vps](deploy/vps/README.md),
the [unit dir](server/deploy), the [workflow](.github/workflows/ci.yml) and the
[roadmap](docs/roadmap.md#backlog). Component: [server](server/README.md).
`)
	writeFile(t, repo, "server/README.md", "# regente-server\n")
	writeFile(t, repo, "install.sh", "#!/bin/sh\n")
	writeFile(t, repo, "deploy/vps/README.md", "# VPS\n")
	writeFile(t, repo, "server/deploy/regente.service", "[Unit]\n")
	writeFile(t, repo, ".github/workflows/ci.yml", "name: CI\n")
	writeFile(t, repo, "docs/roadmap.md", "# Roadmap\n")

	if _, err := Build(repo, out); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	href := regexp.MustCompile(`href="([^"]+)"`)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		body, _ := os.ReadFile(filepath.Join(out, e.Name()))
		for _, m := range href.FindAllStringSubmatch(string(body), -1) {
			target := html.UnescapeString(m[1])
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
				continue
			}
			file, _, _ := strings.Cut(target, "#")
			if file == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(file))); err != nil {
				t.Errorf("%s: href=%q não existe no site gerado", e.Name(), target)
			}
		}
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
