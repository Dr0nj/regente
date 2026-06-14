// Package storage — git helper for F13 GitOps.
//
// Workspace pode ser um clone Git. Quando --git-source é setado:
//   - Boot: clone se vazio; senão fetch+reset --hard.
//   - Polling opcional (--git-poll-interval).
//   - Endpoint /api/git/sync força refresh.
//
// Pitfall conhecido: `git reset --hard` apaga edições locais não commitadas.
// Em modo GitOps puro isso é OK (nada deve ser editado direto em disco).
package storage

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitOps gerencia o workspace como clone Git.
type GitOps struct {
	workspace string
	source    string // ex: git@github.com:owner/repo.git ou https://... (SEMPRE sem credenciais)
	branch    string
	token     string // PAT em memória; auth por header HTTP por comando — NUNCA gravado em .git/config
	mu        sync.Mutex
	lastSync  time.Time
	lastSHA   string
	// F13.4 drift state
	drift     bool
	remoteSHA string
	driftErr  string
}

func NewGitOps(workspace, source, branch string) *GitOps {
	if branch == "" {
		branch = "main"
	}
	return &GitOps{workspace: workspace, source: scrubCredentials(source), branch: branch}
}

// InjectToken guarda o PAT em memória para autenticação por header HTTP
// (Basic x-access-token:<PAT>) em cada comando git. Substitui o comportamento
// antigo de embutir o token na URL do remote (que vazava o PAT para
// .git/config e para o /api/git/status). Também garante que a URL fonte está
// limpa de credenciais legadas.
func (g *GitOps) InjectToken(token string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.token = strings.TrimSpace(token)
	g.source = scrubCredentials(g.source)
}

// dbArtifactPatterns — padrões de arquivos SQLite que NUNCA devem ser
// trackeados/estagiados no workspace GitOps. Se a DB mora dentro do clone,
// `git add -A` a commitaria e o `reset --hard` da daily tropeçaria no WAL
// travado (Windows). Ver bug WAL/DB no handoff.
var dbArtifactPatterns = []string{"*.db", "*.db-wal", "*.db-shm", "*.sqlite", "*.sqlite3"}

// EnsureLocalExcludes garante que artefatos locais (SQLite DB) nunca sejam
// estagiados por `git add -A` nem tocados por `reset --hard`, escrevendo os
// padrões em .git/info/exclude (local ao clone, não-commitado, sobrevive ao
// reset). Defesa que independe de token — complementa o .gitignore versionado.
func (g *GitOps) EnsureLocalExcludes() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.writeLocalExcludesLocked()
}

func (g *GitOps) writeLocalExcludesLocked() error {
	if !g.IsRepo() {
		return nil
	}
	excludePath := filepath.Join(g.workspace, ".git", "info", "exclude")
	existing, _ := os.ReadFile(excludePath)
	content := string(existing)
	const marker = "# regente: SQLite artifacts (never track)"
	if strings.Contains(content, marker) {
		return nil
	}
	var b strings.Builder
	b.WriteString(content)
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(marker + "\n")
	for _, p := range dbArtifactPatterns {
		b.WriteString(p + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(excludePath, []byte(b.String()), 0o644)
}

// CleanupDBArtifacts limpa a poluição de runs antigas que commitaram a SQLite
// DB no repo: (1) garante .gitignore versionado com os padrões; (2) destrackeia
// os arquivos do índice (git rm --cached, só toca o índice — seguro mesmo com
// WAL travado); (3) commita + push se houver token. Idempotente: sem mudanças,
// não commita. Precisa de token para o push (origin fica limpo só com auth).
func (g *GitOps) CleanupDBArtifacts(actor string) (changed bool, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.IsRepo() {
		return false, fmt.Errorf("not a git repo")
	}
	if err := g.writeLocalExcludesLocked(); err != nil {
		return false, fmt.Errorf("local excludes: %w", err)
	}
	// (1) .gitignore versionado — aplica a TODOS os clones (inclusive design sessions).
	giPath := filepath.Join(g.workspace, ".gitignore")
	gi, _ := os.ReadFile(giPath)
	giContent := string(gi)
	const giMarker = "# regente: SQLite artifacts"
	if !strings.Contains(giContent, giMarker) {
		var b strings.Builder
		b.WriteString(giContent)
		if len(giContent) > 0 && !strings.HasSuffix(giContent, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(giMarker + "\n")
		for _, p := range dbArtifactPatterns {
			b.WriteString(p + "\n")
		}
		if err := os.WriteFile(giPath, []byte(b.String()), 0o644); err != nil {
			return false, fmt.Errorf("write .gitignore: %w", err)
		}
	}
	// (2) estagia o .gitignore (novo arquivo — `commit -a` não pegaria) e
	// destrackeia a DB do índice (não apaga do disco; seguro com WAL travado).
	if _, err := g.run("add", ".gitignore"); err != nil {
		return false, fmt.Errorf("git add .gitignore: %w", err)
	}
	for _, p := range dbArtifactPatterns {
		// --ignore-unmatch: sem erro se nenhum arquivo casar.
		_, _ = g.run("rm", "-r", "--cached", "--ignore-unmatch", p)
	}
	// Algo estagiado para commitar?
	staged, err := g.run("diff", "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(staged) == "" {
		return false, nil // nada poluído; tudo certo
	}
	if actor == "" {
		actor = "regente"
	}
	author := fmt.Sprintf("%s <%s>", actor, actor+"@regente.local")
	if _, err := g.run("commit", "-m", "chore: stop tracking SQLite DB artifacts + .gitignore", "--author", author); err != nil {
		return false, fmt.Errorf("commit cleanup: %w", err)
	}
	if g.token == "" {
		// commitou local; sem token não dá pra limpar o origin ainda.
		return true, fmt.Errorf("commit local feito, mas push precisa de token (origin ainda poluído)")
	}
	if _, err := g.run("push", "origin", g.branch); err != nil {
		return true, fmt.Errorf("push cleanup: %w", err)
	}
	g.lastSync = time.Now()
	if err := g.refreshSHA(); err != nil {
		return true, err
	}
	return true, nil
}

// scrubCredentials remove userinfo (token@) de URLs https.
func scrubCredentials(url string) string {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(url, "https://") {
		return url
	}
	rest := strings.TrimPrefix(url, "https://")
	if at := strings.Index(rest, "@"); at >= 0 {
		if slash := strings.Index(rest, "/"); slash == -1 || at < slash {
			rest = rest[at+1:]
		}
	}
	return "https://" + rest
}

// WebURLFromSource converte um git source (https com/sem creds, ou ssh
// git@host:owner/repo.git) na URL web navegável do repositório.
func WebURLFromSource(src string) string {
	s := strings.TrimSuffix(scrubCredentials(src), ".git")
	if strings.HasPrefix(s, "https://") {
		return s
	}
	if strings.HasPrefix(s, "git@") {
		rest := strings.TrimPrefix(s, "git@")
		rest = strings.Replace(rest, ":", "/", 1)
		return "https://" + rest
	}
	return ""
}

// authArgs retorna flags `-c http.extraHeader=...` quando há token e a fonte é
// https. Aplicado a todo comando git que fala com o remote (e inofensivo nos
// locais). Formato aceito pelo GitHub: Basic base64("x-access-token:<PAT>").
func (g *GitOps) authArgs() []string {
	if g.token == "" || !strings.HasPrefix(g.source, "https://") {
		return nil
	}
	b64 := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + g.token))
	return []string{"-c", "http.extraHeader=AUTHORIZATION: basic " + b64}
}

func (g *GitOps) Source() string { return g.source }
func (g *GitOps) Branch() string { return g.branch }

// HasEmbeddedToken — legado. Após o scrub de credenciais a URL nunca carrega
// token; mantido para compat de chamadas (sempre false agora).
func (g *GitOps) HasEmbeddedToken() bool {
	return strings.HasPrefix(g.source, "https://") && strings.Contains(strings.TrimPrefix(g.source, "https://"), "@")
}

// HasAuth reporta se há credencial disponível para push/fetch autenticado.
func (g *GitOps) HasAuth() bool { return g.token != "" }

// extractRemoteToken lê o userinfo de um remote https://<creds>@host/... legado
// e devolve o PAT. Aceita "<token>@" e "<user>:<token>@". "" se não houver.
func (g *GitOps) extractRemoteToken() string {
	out, err := g.run("config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(out)
	if !strings.HasPrefix(url, "https://") {
		return ""
	}
	rest := strings.TrimPrefix(url, "https://")
	at := strings.Index(rest, "@")
	slash := strings.Index(rest, "/")
	if at < 0 || (slash >= 0 && at > slash) {
		return ""
	}
	creds := rest[:at]
	if colon := strings.Index(creds, ":"); colon >= 0 {
		return creds[colon+1:] // user:token → token
	}
	return creds
}

// IsRepo reports if workspace is already a git repo.
func (g *GitOps) IsRepo() bool {
	_, err := os.Stat(filepath.Join(g.workspace, ".git"))
	return err == nil
}

// IsEmpty reports if workspace has no files (besides nothing).
func (g *GitOps) IsEmpty() bool {
	entries, err := os.ReadDir(g.workspace)
	if err != nil {
		return true
	}
	return len(entries) == 0
}

// EnsureClone garante que o workspace é um clone do source/branch.
//
// Comportamento:
//   - Se IsRepo: fetch+reset --hard origin/branch.
//   - Se vazio (não-repo): clone direto para dentro de workspace.
//   - Se NÃO repo e NÃO vazio: erro (proteção contra perda de dados).
func (g *GitOps) EnsureClone() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.source == "" {
		return fmt.Errorf("git source not configured")
	}
	if g.IsRepo() {
		// Higiene de credencial: clones antigos podem ter PAT embutido no
		// remote (.git/config). Migração transparente: se não recebemos token
		// por env/flag mas o remote legado carrega um, extraímos para memória
		// (não quebra quem dependia do token no disco) e então reescrevemos o
		// remote com a URL limpa — auth passa a ser só por header (authArgs).
		if g.token == "" {
			if tok := g.extractRemoteToken(); tok != "" {
				g.token = tok
				log.Printf("[git] PAT legado migrado do .git/config para memória (header-only)")
			}
		}
		if g.source != "" {
			if _, err := g.run("remote", "set-url", "origin", g.source); err != nil {
				return fmt.Errorf("scrub remote url: %w", err)
			}
		}
		if err := g.fetchAndReset(); err != nil {
			return err
		}
		_ = g.writeLocalExcludesLocked() // best-effort: nunca trackear SQLite DB
		return g.refreshSHA()
	}
	if !g.IsEmpty() {
		return fmt.Errorf("workspace %q is not a git repo and not empty; refuse to clobber. Move files away or rm -rf and retry", g.workspace)
	}
	// clone into workspace (clone needs target to be empty/non-existent)
	// trick: clone para temp e move conteúdo, OU usar `git clone <src> .` quando dentro do dir.
	if err := os.MkdirAll(g.workspace, 0o755); err != nil {
		return err
	}
	args := append(g.authArgs(), "clone", "--branch", g.branch, g.source, ".")
	cmd := exec.Command("git", args...)
	cmd.Dir = g.workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w (%s)", err, scrubCredentials(strings.TrimSpace(string(out))))
	}
	_ = g.writeLocalExcludesLocked() // best-effort: nunca trackear SQLite DB
	return g.refreshSHA()
}

// SyncFromRemote roda fetch + reset --hard.
func (g *GitOps) SyncFromRemote() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.IsRepo() {
		return fmt.Errorf("workspace is not a git repo")
	}
	if err := g.fetchAndReset(); err != nil {
		return err
	}
	// refreshSHA must run BEFORE assigning remoteSHA; otherwise we copy the
	// stale lastSHA from before the fetch+reset (regression observed in
	// /api/git/sync response: sha updated but remoteSha still old).
	if err := g.refreshSHA(); err != nil {
		return err
	}
	// F13.4 — sync clears drift; local now matches remote.
	g.drift = false
	g.remoteSHA = g.lastSHA
	g.driftErr = ""
	return nil
}

func (g *GitOps) fetchAndReset() error {
	if _, err := g.run("fetch", "origin", g.branch); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if _, err := g.run("reset", "--hard", "origin/"+g.branch); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	g.lastSync = time.Now()
	return nil
}

func (g *GitOps) refreshSHA() error {
	out, err := g.run("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	g.lastSHA = strings.TrimSpace(out)
	return nil
}

// Status returns current git status snapshot.
type Status struct {
	Configured bool      `json:"configured"`
	Source     string    `json:"source,omitempty"`
	WebURL     string    `json:"webUrl,omitempty"` // URL navegável do repo (deep-links na UI)
	Branch     string    `json:"branch,omitempty"`
	SHA        string    `json:"sha,omitempty"`      // full SHA
	ShortSHA   string    `json:"shortSha,omitempty"` // first 7 chars
	LastSync   time.Time `json:"lastSync,omitempty"`
	// F13.4 drift detection
	Drift     bool   `json:"drift"`               // true if remote ahead of local
	RemoteSHA string `json:"remoteSha,omitempty"` // remote HEAD (if drift check ran)
	DriftErr  string `json:"driftErr,omitempty"`  // last drift check error (transient)
}

func (g *GitOps) Status() Status {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g == nil || g.source == "" {
		return Status{Configured: false}
	}
	short := g.lastSHA
	if len(short) > 7 {
		short = short[:7]
	}
	return Status{
		Configured: true,
		Source:     scrubCredentials(g.source),
		WebURL:     WebURLFromSource(g.source),
		Branch:     g.branch,
		SHA:        g.lastSHA,
		ShortSHA:   short,
		LastSync:   g.lastSync,
		Drift:      g.drift,
		RemoteSHA:  g.remoteSHA,
		DriftErr:   g.driftErr,
	}
}

// CheckDrift does a lightweight `git fetch` + compares local HEAD vs origin/<branch>.
// Updates internal drift fields. Safe for polling.
func (g *GitOps) CheckDrift() (drifted bool, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.IsRepo() {
		return false, fmt.Errorf("not a git repo")
	}
	if _, err := g.run("fetch", "origin", g.branch); err != nil {
		g.driftErr = err.Error()
		return false, err
	}
	remoteOut, err := g.run("rev-parse", "origin/"+g.branch)
	if err != nil {
		g.driftErr = err.Error()
		return false, err
	}
	localOut, err := g.run("rev-parse", "HEAD")
	if err != nil {
		g.driftErr = err.Error()
		return false, err
	}
	remote := strings.TrimSpace(remoteOut)
	local := strings.TrimSpace(localOut)
	g.remoteSHA = remote
	g.driftErr = ""
	g.drift = remote != local
	return g.drift, nil
}

// run executes git in workspace dir. Auth (quando configurada) entra como
// `-c http.extraHeader=...` por comando — o token nunca toca o disco.
func (g *GitOps) run(args ...string) (string, error) {
	full := append(g.authArgs(), args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = g.workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %v: %w (%s)", args, err, scrubCredentials(strings.TrimSpace(string(out))))
	}
	return string(out), nil
}

// === F13.2 — write-side primitives (branch + commit + push) ===

// CheckoutNewBranch cria branch a partir de origin/<branch> e checa out.
func (g *GitOps) CheckoutNewBranch(branchName string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.IsRepo() {
		return fmt.Errorf("not a git repo")
	}
	if _, err := g.run("fetch", "origin", g.branch); err != nil {
		return err
	}
	if _, err := g.run("checkout", "-B", branchName, "origin/"+g.branch); err != nil {
		return err
	}
	return nil
}

// CheckoutLocalBranch cria branch a partir do HEAD atual SEM fetch/reset —
// preserva o working tree (necessário para publish de design sessions, onde
// edições já estão em disco e não devem ser perdidas).
func (g *GitOps) CheckoutLocalBranch(branchName string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.IsRepo() {
		return fmt.Errorf("not a git repo")
	}
	if _, err := g.run("checkout", "-b", branchName); err != nil {
		return err
	}
	return nil
}

// HeadSHA retorna o sha do HEAD local atual (sem fetch).
func (g *GitOps) HeadSHA() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out, err := g.run("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// IsClean retorna true se o working tree não tem mudanças (porcelain vazio).
// P4 (2026-04-26) — usado para detectar empty publish.
func (g *GitOps) IsClean() (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out, err := g.run("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

// AheadBehind retorna (ahead, behind) entre HEAD local e origin/<branch>.
// Faz `git fetch origin <branch>` antes da contagem (assim a info reflete o
// estado real do remote no momento da chamada).
//
// P8 (2026-04-26) — badge ahead/behind por design session.
func (g *GitOps) AheadBehind() (ahead, behind int, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.IsRepo() {
		return 0, 0, fmt.Errorf("not a git repo")
	}
	if _, err := g.run("fetch", "origin", g.branch); err != nil {
		return 0, 0, err
	}
	out, err := g.run("rev-list", "--left-right", "--count", "HEAD..."+"origin/"+g.branch)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output: %q", out)
	}
	a, err1 := strconv.Atoi(parts[0])
	b, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("parse rev-list: %v / %v", err1, err2)
	}
	return a, b, nil
}

// AddAndCommit adiciona path e commita com message + author.
// authorName/email opcional; se vazio usa "regente <bot@regente>".
func (g *GitOps) AddAndCommit(path, message, authorName, authorEmail string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if authorName == "" {
		authorName = "regente"
	}
	if authorEmail == "" {
		authorEmail = "bot@regente.local"
	}
	rel := path
	if filepath.IsAbs(path) {
		if r, err := filepath.Rel(g.workspace, path); err == nil {
			rel = r
		}
	}
	// stage (works for adds, modifies, AND deletes)
	if _, err := g.run("add", "-A", rel); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	// commit with author override
	author := fmt.Sprintf("%s <%s>", authorName, authorEmail)
	out, err := g.run("commit", "-m", message, "--author", author, "--allow-empty")
	if err != nil {
		return fmt.Errorf("git commit: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// PushBranch faz push da branch local pro origin (set-upstream).
func (g *GitOps) PushBranch(branchName string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, err := g.run("push", "-u", "origin", branchName); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// RebaseBranch rebases current branch onto origin/<base> (F13.4 conflict handling).
// Caller should abort if this fails (true merge conflict).
func (g *GitOps) RebaseBranch(base string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, err := g.run("fetch", "origin", base); err != nil {
		return fmt.Errorf("fetch for rebase: %w", err)
	}
	if _, err := g.run("rebase", "origin/"+base); err != nil {
		return fmt.Errorf("rebase: %w", err)
	}
	return nil
}

// AbortRebase cancels an in-progress rebase (F13.4).
func (g *GitOps) AbortRebase() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, err := g.run("rebase", "--abort")
	return err
}

// CheckoutMain volta para a branch principal e reseta ao remote (descarta
// qualquer mudança local da branch anterior).
func (g *GitOps) CheckoutMain() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, err := g.run("checkout", g.branch); err != nil {
		// se branch principal não existe localmente, cria a partir do remote
		if _, e2 := g.run("checkout", "-B", g.branch, "origin/"+g.branch); e2 != nil {
			return fmt.Errorf("checkout main: %w", e2)
		}
	}
	if _, err := g.run("reset", "--hard", "origin/"+g.branch); err != nil {
		return fmt.Errorf("reset main: %w", err)
	}
	return g.refreshSHA()
}

// DirectPush does add-all + commit + push to the tracked branch (main).
// Used in direct write-mode when gitOps is active.
func (g *GitOps) DirectPush(message, actor string) error {
	if actor == "" {
		actor = "regente"
	}
	if err := g.AddAndCommit(".", message, actor, actor+"@regente.local"); err != nil {
		return fmt.Errorf("direct commit: %w", err)
	}
	if err := g.PushBranch(g.branch); err != nil {
		return fmt.Errorf("direct push: %w", err)
	}
	g.mu.Lock()
	g.lastSync = time.Now()
	g.mu.Unlock()
	return g.refreshSHA()
}

// SafeBranchName slugifica um string para nome de branch git válido.
func SafeBranchName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '/' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	out = strings.Trim(out, "-/.")
	if out == "" {
		out = "x"
	}
	return out
}
