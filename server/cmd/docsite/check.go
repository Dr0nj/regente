package main

// DOC-E: valida a fonte e compara o site inteiro com uma geração isolada.
// Não repara HTML nem modifica o checkout durante a verificação.
import (
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
)

type htmlReferences struct {
	targets []string
	ids     map[string]bool
}

func references(body []byte) htmlReferences {
	result := htmlReferences{ids: map[string]bool{}}
	z := html.NewTokenizer(bytes.NewReader(body))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return result
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			for _, a := range t.Attr {
				if a.Key == "id" {
					result.ids[a.Val] = true
				}
				if a.Key == "href" || a.Key == "src" {
					result.targets = append(result.targets, a.Val)
				}
			}
		}
	}
}

func localTarget(root, source, target string) (string, string, bool, error) {
	u, err := url.Parse(target)
	if err != nil {
		return "", "", false, err
	}
	if u.IsAbs() || u.Host != "" || strings.HasPrefix(target, "//") {
		return "", "", false, nil
	}
	file := source
	if u.Path != "" {
		file = filepath.Join(filepath.Dir(source), filepath.FromSlash(u.Path))
	}
	full := filepath.Join(root, file)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", false, fmt.Errorf("target escapes documentation root")
	}
	if _, err := os.Stat(full); err != nil {
		return "", "", false, err
	}
	return filepath.Clean(file), u.Fragment, true, nil
}

// Estado e outputs ignorados pelo Git não são fontes; usado também pelo gerador.
func ignoredSourceDir(rel string) bool {
	switch filepath.Base(rel) {
	case ".git", "node_modules", "vendor", "dist", ".integration", ".smoke", "test-results", "playwright-report", "site":
		return true
	}
	switch rel {
	case "server/workspace", "server/sessions", "sessions", "deploy/demo/sessions":
		return true
	}
	return false
}

func checkSourceLinks(repo string) error {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()))
	return filepath.WalkDir(repo, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(repo, p)
			if p != repo && ignoredSourceDir(filepath.ToSlash(rel)) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || filepath.Ext(p) != ".md" {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var body bytes.Buffer
		if err := md.Convert(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), &body); err != nil {
			return err
		}
		source, _ := filepath.Rel(repo, p)
		for _, target := range references(body.Bytes()).targets {
			// Fragmentos Markdown do GitHub e do Goldmark podem diferir. A catraca
			// de âncoras abaixo usa os IDs efetivamente gerados no site publicado.
			if _, _, _, err := localTarget(repo, source, target); err != nil {
				return fmt.Errorf("source link %s -> %q: %w", source, target, err)
			}
		}
		return nil
	})
}

func checkSiteLinks(site string) error {
	pages := map[string]htmlReferences{}
	err := filepath.WalkDir(site, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(p) == ".html" {
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(site, p)
			pages[rel] = references(raw)
		}
		return nil
	})
	if err != nil {
		return err
	}
	var names []string
	for name := range pages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, source := range names {
		for _, target := range pages[source].targets {
			file, anchor, local, err := localTarget(site, source, target)
			if err != nil {
				return fmt.Errorf("site link %s -> %q: %w", source, target, err)
			}
			if page, ok := pages[file]; local && ok && anchor != "" && !page.ids[anchor] {
				return fmt.Errorf("site link %s -> %q: missing anchor", source, target)
			}
		}
	}
	return nil
}

func siteFiles(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected symlink in generated site: %s", p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		switch filepath.Ext(p) {
		case ".html", ".json", ".yaml", ".css", ".js", ".svg":
			b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		}
		name, _ := filepath.Rel(root, p)
		files[name] = b
		return nil
	})
	return files, err
}

func compareSites(wantDir, gotDir string) error {
	want, err := siteFiles(wantDir)
	if err != nil {
		return err
	}
	got, err := siteFiles(gotDir)
	if err != nil {
		return err
	}
	var differences []string
	for name, raw := range want {
		if other, ok := got[name]; !ok || !bytes.Equal(raw, other) {
			differences = append(differences, "missing or stale: "+filepath.ToSlash(name))
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			differences = append(differences, "unexpected generated file: "+filepath.ToSlash(name))
		}
	}
	sort.Strings(differences)
	if len(differences) != 0 {
		return fmt.Errorf("generated site differs: %s", strings.Join(differences, "; "))
	}
	return nil
}

// Check valida links e o manifesto de arquivos, sem substituir o site versionado.
func Check(repoDir, siteDir string) error {
	repo, err := filepath.Abs(repoDir)
	if err != nil {
		return err
	}
	if err := checkSourceLinks(repo); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "regente-docsite-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if _, err := Build(repo, temp); err != nil {
		return err
	}
	if err := checkSiteLinks(temp); err != nil {
		return err
	}
	return compareSites(temp, siteDir)
}
