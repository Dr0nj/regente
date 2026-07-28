// docsite — ADV-7: gerador do site de docs do Regente.
//
// Converte os markdown que JÁ SÃO a documentação do projeto (README do
// monorepo + READMEs dos componentes + docs/*.md) num site estático
// SELF-CONTAINED: CSS inline, zero JS, zero CDN — o mesmo princípio do
// hosting single-origin do SPA (-spa-dir). O server serve o resultado em
// /docs com a flag -docs-dir; o site também funciona aberto direto do disco
// (file://) ou publicado em qualquer static host.
//
//	go run ./cmd/docsite -repo .. -out ../docs/site
//
// Decisões:
//   - Docs-as-code: NÃO existe conteúdo próprio do site — markdown do repo é
//     a fonte única; o site é 100% derivado (roda de novo = atualizado).
//   - Links entre os .md são reescritos pros .html correspondentes; âncoras
//     preservadas. Imagens locais referenciadas são copiadas pro site.
//   - goldmark com GFM (tabelas) + WithUnsafe (os READMEs usam HTML cru).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Dr0nj/regente-server/internal/api"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

func main() {
	repo := flag.String("repo", ".", "monorepo root (where README.md lives)")
	out := flag.String("out", "docs/site", "output directory for the site")
	flag.Parse()
	n, err := Build(*repo, *out)
	if err != nil {
		log.Fatalf("docsite: %v", err)
	}
	log.Printf("docsite: %d page(s) generated in %s", n, *out)
}

// page — um markdown do repo que vira página.
type page struct {
	src   string // caminho relativo à raiz do repo (barra /)
	slug  string // nome do .html de saída (sem extensão)
	title string // 1º heading; fallback = slug
	order int    // posição na nav (menor primeiro)
	html  []byte
}

// knownOrder fixa o topo da navegação; o resto entra alfabético depois.
// A referência de API entra no meio (apiNavOrder) — não vem de markdown.
var knownOrder = map[string]int{
	"README.md":        0,
	"server/README.md": 1,
	"agent/README.md":  2,
	"app/README.md":    3,
	"deploy/README.md": 4,
	// 5 = API reference (apiNavOrder)
	"docs/operations.md":          6,
	"docs/conditions-events.md":   7,
	"docs/dr-backup.md":           8,
	"docs/slos.md":                9,
	"docs/mcp.md":                 10,
	"docs/architecture-future.md": 11,
	"docs/case-study.en.md":       12,
}

// skipDoc — markdown do repo que NÃO vai pro site público: documento de
// trabalho interno (fica em pt-BR de propósito) ou rascunho. O site é a cara
// do produto, e o produto fala inglês.
var skipDoc = map[string]bool{
	"docs/roadmap.md":                      true, // planejamento nosso, pt-BR
	"docs/linkedin-post.md":                true, // rascunhos de divulgação
	"docs/case-study.md":                   true, // versão PT (a EN é publicada)
	"docs/monitoring-immutability-plan.md": true, // plano interno, pt-BR
	"docs/plano-layout-jobs.md":            true, // plano interno, pt-BR
}

// Build gera o site a partir do repo. Retorna o nº de páginas.
func Build(repoDir, outDir string) (int, error) {
	pages, err := collect(repoDir)
	if err != nil {
		return 0, err
	}
	if len(pages) == 0 {
		return 0, fmt.Errorf("no markdown found in %s", repoDir)
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()), // READMEs usam HTML cru (img/div)
	)

	// slug por src — a tabela de reescrita de links.
	slugBySrc := map[string]string{}
	for _, p := range pages {
		slugBySrc[p.src] = p.slug
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 0, err
	}
	for i := range pages {
		raw, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(pages[i].src)))
		if err != nil {
			return 0, err
		}
		pages[i].title = firstHeading(raw, pages[i].slug)
		var buf bytes.Buffer
		if err := md.Convert(raw, &buf); err != nil {
			return 0, fmt.Errorf("%s: %w", pages[i].src, err)
		}
		body := rewriteLinks(buf.Bytes(), pages[i].src, repoDir, slugBySrc)
		body = copyAssets(body, pages[i].src, repoDir, outDir)
		pages[i].html = body
	}
	sort.Slice(pages, func(a, b int) bool {
		if pages[a].order != pages[b].order {
			return pages[a].order < pages[b].order
		}
		return pages[a].slug < pages[b].slug
	})

	for _, p := range pages {
		out := filepath.Join(outDir, p.slug+".html")
		if err := os.WriteFile(out, renderPage(p, pages), 0o644); err != nil {
			return 0, err
		}
	}
	if err := writeAPIReference(outDir); err != nil {
		return 0, fmt.Errorf("api reference: %w", err)
	}
	return len(pages) + 1, nil
}

// apiNavOrder — posição da referência de API na navegação (entre os READMEs
// dos componentes e os docs). Ver knownOrder.
const apiNavOrder = 5

// writeAPIReference emite a referência de API como página ESTÁTICA: o MESMO
// viewer que o server serve em /api-docs, com a spec INJETADA num
// <script type="application/json"> — em file:// o fetch é bloqueado por CORS, e
// num static host não há API atrás da página pra alimentar o Try-it (o viewer
// detecta o node injetado e entra em modo estático sozinho). openapi.yaml/json
// vão junto: são os links do header e o que se importa em Postman/codegen.
func writeAPIReference(outDir string) error {
	viewer, specYAML, specJSON, err := api.OpenAPIAssets()
	if err != nil {
		return err
	}
	inline := []byte(`<script id="openapi-inline" type="application/json">` +
		scriptSafeJSON(specJSON) + "</script>\n</head>")
	html := bytes.Replace(viewer, []byte("</head>"), inline, 1)
	html = bytes.Replace(html, []byte(`<div class="links">`),
		[]byte(`<div class="links"><a href="index.html">← Docs</a> · `), 1)

	for name, body := range map[string][]byte{
		"api.html":     html,
		"openapi.yaml": specYAML,
		"openapi.json": specJSON,
	} {
		if err := os.WriteFile(filepath.Join(outDir, name), body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// scriptSafeJSON escapa o que fecharia o <script> mais cedo (um "</script>"
// dentro de alguma description da spec). JSON aceita \uXXXX dentro de string,
// e fora de string esses bytes nao aparecem - a troca e segura e o JSON.parse
// do viewer enxerga exatamente o mesmo documento.
func scriptSafeJSON(b []byte) string {
	return strings.NewReplacer(
		"<", "\\u003c", ">", "\\u003e", "&", "\\u0026",
	).Replace(string(b))
}

// collect acha os markdown que viram página: README.md, */README.md
// (componentes de 1º nível) e docs/*.md.
func collect(repoDir string) ([]page, error) {
	var srcs []string
	if _, err := os.Stat(filepath.Join(repoDir, "README.md")); err == nil {
		srcs = append(srcs, "README.md")
	}
	entries, _ := os.ReadDir(repoDir)
	for _, e := range entries {
		if e.IsDir() && e.Name() != "docs" && !strings.HasPrefix(e.Name(), ".") {
			if _, err := os.Stat(filepath.Join(repoDir, e.Name(), "README.md")); err == nil {
				srcs = append(srcs, e.Name()+"/README.md")
			}
		}
	}
	docs, _ := os.ReadDir(filepath.Join(repoDir, "docs"))
	for _, e := range docs {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") && !skipDoc["docs/"+e.Name()] {
			srcs = append(srcs, "docs/"+e.Name())
		}
	}

	var pages []page
	for _, src := range srcs {
		slug := "index"
		if src != "README.md" {
			base := strings.TrimSuffix(path.Base(src), ".md")
			if base == "README" {
				base = path.Base(path.Dir(src)) // server/README.md → server
			}
			slug = base
		}
		order, ok := knownOrder[src]
		if !ok {
			order = 100
		}
		pages = append(pages, page{src: src, slug: slug, order: order})
	}
	return pages, nil
}

// firstHeading extrai o primeiro "# ..." do markdown (ou <h1>...</h1> de HTML
// cru, caso do README do monorepo).
func firstHeading(raw []byte, fallback string) string {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return cleanTitle(strings.TrimPrefix(line, "# "))
		}
		if m := regexp.MustCompile(`<h1>(.+?)</h1>`).FindStringSubmatch(line); m != nil {
			return cleanTitle(m[1])
		}
	}
	return fallback
}

// cleanTitle remove markup leve (emoji fica — identidade do projeto).
func cleanTitle(s string) string {
	s = regexp.MustCompile("[`*_]").ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

var hrefRe = regexp.MustCompile(`(href|src)="([^"]+)"`)

// rewriteLinks converte href de .md relativos pro .html correspondente,
// resolvendo o caminho a partir do DIRETÓRIO da página de origem
// (docs/x.md com href ../README.md → index.html). Âncoras preservadas.
//
// Alvo que NÃO virou página (script, diretório, README de subpasta, o roadmap
// que o skipDoc tira) vira link ABSOLUTO pro GitHub: caminho relativo do repo
// não existe no site publicado nem no docs/site/ aberto do disco — antes disso
// eram 404 (6 das 13 páginas tinham, a index sozinha tinha 8).
func rewriteLinks(body []byte, src, repoDir string, slugBySrc map[string]string) []byte {
	dir := path.Dir(src)
	return hrefRe.ReplaceAllFunc(body, func(m []byte) []byte {
		g := hrefRe.FindSubmatch(m)
		attr, target := string(g[1]), string(g[2])
		if attr != "href" || strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			return m
		}
		base, anchor, _ := strings.Cut(target, "#")
		resolved := path.Clean(path.Join(dir, base))
		// href="server" (diretório) e href="server/README.md" apontam pro mesmo lugar.
		slug, ok := slugBySrc[resolved]
		if !ok {
			slug, ok = slugBySrc[resolved+"/README.md"]
		}
		if !ok {
			return []byte(`href="` + repoLink(resolved, anchor, repoDir) + `"`)
		}
		out := slug + ".html"
		if anchor != "" {
			out += "#" + anchor
		}
		return []byte(`href="` + out + `"`)
	})
}

// repoBlobBase — onde o alvo não-publicado é lido: o próprio repositório.
const repoBlobBase = "https://github.com/Dr0nj/regente"

// repoLink monta a URL do GitHub pro caminho do repo (blob p/ arquivo, tree p/
// diretório). Caminho que não existe no disco fica como está — é link quebrado
// no markdown, e mascarar isso só esconderia o defeito.
func repoLink(resolved, anchor, repoDir string) string {
	st, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(resolved)))
	if err != nil {
		if anchor != "" {
			return resolved + "#" + anchor
		}
		return resolved
	}
	kind := "blob"
	if st.IsDir() {
		kind = "tree"
	}
	url := repoBlobBase + "/" + kind + "/main/" + resolved
	if anchor != "" {
		url += "#" + anchor
	}
	return url
}

// copyAssets copia imagens locais referenciadas (src="...") pro site,
// preservando o caminho relativo À RAIZ (a página reescreve o src pra ele).
func copyAssets(body []byte, src, repoDir, outDir string) []byte {
	dir := path.Dir(src)
	return hrefRe.ReplaceAllFunc(body, func(m []byte) []byte {
		g := hrefRe.FindSubmatch(m)
		attr, target := string(g[1]), string(g[2])
		if attr != "src" || strings.Contains(target, "://") || strings.HasPrefix(target, "data:") {
			return m
		}
		resolved := path.Clean(path.Join(dir, target))
		from := filepath.Join(repoDir, filepath.FromSlash(resolved))
		if _, err := os.Stat(from); err != nil {
			return m
		}
		to := filepath.Join(outDir, filepath.FromSlash(resolved))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err == nil {
			if b, err := os.ReadFile(from); err == nil {
				_ = os.WriteFile(to, b, 0o644)
			}
		}
		return []byte(`src="` + resolved + `"`)
	})
}

// renderPage monta o HTML final: sidebar de navegação + conteúdo, CSS inline.
func renderPage(p page, all []page) []byte {
	var nav strings.Builder
	apiWritten := false
	writeAPILink := func() {
		nav.WriteString(`<a href="api.html">API reference</a>` + "\n")
		apiWritten = true
	}
	for _, q := range all {
		// A referência de API não vem de markdown — entra na posição fixa.
		if !apiWritten && q.order > apiNavOrder {
			writeAPILink()
		}
		cls := ""
		if q.slug == p.slug {
			cls = ` class="active"`
		}
		fmt.Fprintf(&nav, `<a href="%s.html"%s>%s</a>`+"\n", q.slug, cls, html.EscapeString(q.title))
	}
	if !apiWritten {
		writeAPILink()
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s · Regente Docs</title>
<style>%s</style>
</head>
<body>
<nav class="side">
<div class="brand">🎼 Regente <span>docs</span></div>
%s</nav>
<main>
<article>
%s</article>
<footer>Generated by <code>regente docsite</code> — docs-as-code: edit the markdown in the repo, not this HTML.</footer>
</main>
</body>
</html>
`, html.EscapeString(p.title), siteCSS, nav.String(), p.html)
	return b.Bytes()
}

// siteCSS — tema escuro alinhado à identidade do produto (accent verde),
// inline de propósito: cada página é auto-suficiente, zero requests externos.
const siteCSS = `
:root{--bg:#0b0f0d;--panel:#111613;--border:#1f2a24;--text:#d7e2dc;--muted:#8aa096;--accent:#11c76f;--code:#0f1512}
*{box-sizing:border-box}
body{margin:0;display:flex;min-height:100vh;background:var(--bg);color:var(--text);
  font:15px/1.65 -apple-system,"Segoe UI",Roboto,Ubuntu,sans-serif}
nav.side{position:sticky;top:0;align-self:flex-start;height:100vh;overflow-y:auto;flex:0 0 240px;
  padding:20px 14px;border-right:1px solid var(--border);background:var(--panel)}
nav.side .brand{font-weight:700;font-size:16px;margin-bottom:14px}
nav.side .brand span{color:var(--muted);font-weight:400;font-size:12px}
nav.side a{display:block;padding:6px 10px;margin:2px 0;border-radius:6px;color:var(--muted);
  text-decoration:none;font-size:13px}
nav.side a:hover{color:var(--text);background:rgba(17,199,111,.08)}
nav.side a.active{color:var(--accent);background:rgba(17,199,111,.12);font-weight:600}
main{flex:1;min-width:0;padding:32px 40px}
article{max-width:880px;margin:0 auto}
h1,h2,h3,h4{line-height:1.3;scroll-margin-top:16px}
h1{font-size:28px;border-bottom:1px solid var(--border);padding-bottom:8px}
h2{font-size:21px;margin-top:36px;border-bottom:1px solid var(--border);padding-bottom:6px}
h3{font-size:17px;margin-top:28px}
a{color:var(--accent)}
code{background:var(--code);border:1px solid var(--border);border-radius:4px;padding:1px 5px;
  font:12.5px/1.5 ui-monospace,Consolas,monospace}
pre{background:var(--code);border:1px solid var(--border);border-radius:8px;padding:14px;overflow-x:auto}
pre code{background:none;border:none;padding:0}
table{border-collapse:collapse;width:100%;display:block;overflow-x:auto;font-size:13.5px}
th,td{border:1px solid var(--border);padding:7px 10px;text-align:left;vertical-align:top}
th{background:var(--panel)}
blockquote{margin:0;padding:8px 16px;border-left:3px solid var(--accent);background:var(--panel);
  border-radius:0 6px 6px 0;color:var(--muted)}
img{max-width:100%}
hr{border:none;border-top:1px solid var(--border);margin:28px 0}
footer{max-width:880px;margin:48px auto 0;padding-top:14px;border-top:1px solid var(--border);
  color:var(--muted);font-size:12px}
@media (max-width:760px){body{flex-direction:column}nav.side{position:static;height:auto;flex:none;
  width:100%;border-right:none;border-bottom:1px solid var(--border)}main{padding:20px 16px}}
`
