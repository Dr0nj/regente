// Package storage — git helper for F13 GitOps.
//
// Workspace pode ser um clone Git. Quando --git-source é setado:
//   - Boot: clone se vazio; senão fetch+reset --hard.
//   - Polling opcional (--git-poll-interval).
//   - Endpoint /api/git/sync força refresh.
//
// Implementação Go-nativa via github.com/go-git/go-git/v5 — NÃO há shell-out para
// o binário `git`. Isso permite empacotar o server em imagem distroless (sem git
// no PATH). Auth https usa Basic (x-access-token:<PAT>) por operação; o token
// nunca toca o disco (mesma garantia do header HTTP que substituiu).
//
// Pitfalls do go-git tratados aqui (divergem do binário git):
//   - reset --hard: o HardReset do go-git APAGA arquivos untracked/ignored. O
//     `git` real não toca neles. Reproduzimos o comportamento do git limitando o
//     reset à união {paths do tree alvo} ∪ {paths no índice} (ver hardResetTo).
//   - add -A: o AddWithOptions{All} só consulta w.Excludes (não lê .gitignore /
//     .git/info/exclude). Populamos Excludes com gitignore.ReadPatterns para que
//     os artefatos SQLite (dbArtifactPatterns) nunca sejam estagiados.
//
// `git reset --hard` ainda descarta edições locais COMMITADAS/TRACKED não
// sincronizadas. Em modo GitOps puro isso é OK (nada deve ser editado direto em
// disco); arquivos untracked (ex.: a SQLite DB) são preservados.
package storage

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/utils/merkletrie"
)

// GitOps gerencia o workspace como clone Git.
type GitOps struct {
	workspace string
	source    string // ex: git@github.com:owner/repo.git ou https://... (SEMPRE sem credenciais)
	branch    string
	token     string // PAT em memória; auth por Basic HTTP por operação — NUNCA gravado em .git/config
	mu        sync.Mutex
	lastSync  time.Time
	lastSHA   string
	// F13.4 drift state
	drift     bool
	remoteSHA string
	driftErr  string
	// rebase abort state (replay linear nativo — ver RebaseBranch/AbortRebase)
	rebaseOrig plumbing.Hash
	rebaseRef  plumbing.ReferenceName
}

func NewGitOps(workspace, source, branch string) *GitOps {
	if branch == "" {
		branch = "main"
	}
	return &GitOps{workspace: workspace, source: scrubCredentials(source), branch: branch}
}

// InjectToken guarda o PAT em memória para autenticação Basic (x-access-token:<PAT>)
// em cada operação git que fala com o remote. Substitui o comportamento antigo de
// embutir o token na URL do remote (que vazava o PAT para .git/config e para o
// /api/git/status). Também garante que a URL fonte está limpa de credenciais legadas.
func (g *GitOps) InjectToken(token string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.token = strings.TrimSpace(token)
	g.source = scrubCredentials(g.source)
}

// dbArtifactPatterns — padrões de arquivos SQLite que NUNCA devem ser
// trackeados/estagiados no workspace GitOps. Se a DB mora dentro do clone,
// `add -A` a commitaria e o `reset --hard` da daily tropeçaria no WAL
// travado (Windows). Ver bug WAL/DB no handoff.
var dbArtifactPatterns = []string{"*.db", "*.db-wal", "*.db-shm", "*.sqlite", "*.sqlite3"}

// EnsureLocalExcludes garante que artefatos locais (SQLite DB) nunca sejam
// estagiados por `add -A` nem tocados por `reset --hard`, escrevendo os
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
// os arquivos do índice (equivalente a `rm --cached`, só toca o índice — seguro
// mesmo com WAL travado); (3) commita + push se houver token. Idempotente: sem
// mudanças, não commita. Precisa de token para o push (origin fica limpo só com auth).
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
	repo, err := g.openRepo()
	if err != nil {
		return false, err
	}
	wt, err := g.worktree(repo)
	if err != nil {
		return false, err
	}
	// (2) estagia o .gitignore (novo arquivo) e destrackeia a DB do índice
	// (não apaga do disco; seguro com WAL travado).
	if _, err := wt.Add(".gitignore"); err != nil {
		return false, fmt.Errorf("git add .gitignore: %w", err)
	}
	if err := removeCachedLocked(repo, dbArtifactPatterns); err != nil {
		return false, fmt.Errorf("untrack db artifacts: %w", err)
	}
	// Algo estagiado para commitar?
	staged, err := hasStaged(wt)
	if err != nil {
		return false, err
	}
	if !staged {
		return false, nil // nada poluído; tudo certo
	}
	if actor == "" {
		actor = "regente"
	}
	sig := &object.Signature{Name: actor, Email: actor + "@regente.local", When: time.Now()}
	if _, err := wt.Commit("chore: stop tracking SQLite DB artifacts + .gitignore",
		&git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		return false, fmt.Errorf("commit cleanup: %w", err)
	}
	if g.token == "" {
		// commitou local; sem token não dá pra limpar o origin ainda.
		return true, fmt.Errorf("local commit done, but the push needs a token (origin still polluted)")
	}
	if err := g.pushLocked(repo, g.branch); err != nil {
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

// auth retorna o método de autenticação go-git quando há token e a fonte é https.
// GitHub aceita Basic auth com usuário arbitrário + PAT como senha — equivale ao
// header legado `Authorization: Basic base64("x-access-token:<PAT>")`. Para fontes
// ssh/sem token retorna nil (transporte default).
func (g *GitOps) auth() transport.AuthMethod {
	if g.token == "" || !strings.HasPrefix(g.source, "https://") {
		return nil
	}
	return &githttp.BasicAuth{Username: "x-access-token", Password: g.token}
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

// === helpers go-git ===

func (g *GitOps) openRepo() (*git.Repository, error) {
	return git.PlainOpen(g.workspace)
}

// worktree abre o worktree com os padrões de ignore (.gitignore + .git/info/exclude)
// carregados em Excludes. Necessário porque AddWithOptions{All} no go-git só
// consulta w.Excludes — sem isso os artefatos SQLite seriam estagiados.
func (g *GitOps) worktree(repo *git.Repository) (*git.Worktree, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	if pats, perr := gitignore.ReadPatterns(wt.Filesystem, nil); perr == nil {
		wt.Excludes = append(wt.Excludes, pats...)
	}
	return wt, nil
}

func branchRefName(b string) plumbing.ReferenceName { return plumbing.NewBranchReferenceName(b) }
func remoteRefName(b string) plumbing.ReferenceName {
	return plumbing.NewRemoteReferenceName("origin", b)
}

// fetchLocked roda o equivalente a `git fetch origin <branch>`, criando/atualizando
// refs/remotes/origin/<branch>. NoErrAlreadyUpToDate não é erro.
func (g *GitOps) fetchLocked(repo *git.Repository, branch string) error {
	refspec := config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch))
	err := repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refspec},
		Auth:       g.auth(),
		Force:      true,
		Tags:       git.NoTags,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return err
	}
	return nil
}

func (g *GitOps) refHash(repo *git.Repository, name plumbing.ReferenceName) (plumbing.Hash, error) {
	ref, err := repo.Reference(name, true)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return ref.Hash(), nil
}

// hardResetTo replica `git reset --hard <commit>`: reseta arquivos TRACKED ao
// estado de <commit> e move a branch atual, MAS preserva untracked/ignored. O
// HardReset cru do go-git apaga untracked — por isso limitamos o reset à união
// {paths do tree alvo} ∪ {paths no índice} (que cobre todo tracked e exclui
// untracked).
func (g *GitOps) hardResetTo(repo *git.Repository, wt *git.Worktree, commit plumbing.Hash) error {
	files, err := trackedAndTargetPaths(repo, commit)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		// Sem nada tracked (repo recém-criado): só move a branch do HEAD; passar
		// Files vazio faria o go-git apagar todo untracked. Evitamos isso.
		head, herr := repo.Head()
		if herr != nil {
			return herr
		}
		return repo.Storer.SetReference(plumbing.NewHashReference(head.Name(), commit))
	}
	return wt.Reset(&git.ResetOptions{Commit: commit, Mode: git.HardReset, Files: files})
}

func trackedAndTargetPaths(repo *git.Repository, commit plumbing.Hash) ([]string, error) {
	set := map[string]struct{}{}
	c, err := repo.CommitObject(commit)
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	if err := tree.Files().ForEach(func(f *object.File) error {
		set[f.Name] = struct{}{}
		return nil
	}); err != nil {
		return nil, err
	}
	if idx, err := repo.Storer.Index(); err == nil {
		for _, e := range idx.Entries {
			set[e.Name] = struct{}{}
		}
	}
	files := make([]string, 0, len(set))
	for p := range set {
		files = append(files, p)
	}
	return files, nil
}

// removeCachedLocked replica `git rm --cached <patterns>`: remove do índice as
// entradas cujo basename casa com algum padrão, sem apagar do disco.
func removeCachedLocked(repo *git.Repository, patterns []string) error {
	idx, err := repo.Storer.Index()
	if err != nil {
		return err
	}
	n := 0
	changed := false
	for _, e := range idx.Entries {
		base := filepath.Base(e.Name)
		match := false
		for _, p := range patterns {
			if ok, _ := filepath.Match(p, base); ok {
				match = true
				break
			}
		}
		if match {
			changed = true
			continue
		}
		idx.Entries[n] = e
		n++
	}
	if !changed {
		return nil
	}
	idx.Entries = idx.Entries[:n]
	return repo.Storer.SetIndex(idx)
}

// hasStaged reporta se o índice difere do HEAD (equivalente a `diff --cached`
// não-vazio).
func hasStaged(wt *git.Worktree) (bool, error) {
	st, err := wt.Status()
	if err != nil {
		return false, err
	}
	for _, s := range st {
		if s.Staging != git.Unmodified && s.Staging != git.Untracked {
			return true, nil
		}
	}
	return false, nil
}

// extractRemoteTokenLocked lê o userinfo de um remote https://<creds>@host/... legado
// e devolve o PAT. Aceita "<token>@" e "<user>:<token>@". "" se não houver.
func (g *GitOps) extractRemoteTokenLocked(repo *git.Repository) string {
	cfg, err := repo.Config()
	if err != nil {
		return ""
	}
	rc, ok := cfg.Remotes["origin"]
	if !ok || len(rc.URLs) == 0 {
		return ""
	}
	url := strings.TrimSpace(rc.URLs[0])
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

// setRemoteURLLocked reescreve a URL do remote origin (higiene de credencial).
func (g *GitOps) setRemoteURLLocked(repo *git.Repository, url string) error {
	cfg, err := repo.Config()
	if err != nil {
		return err
	}
	rc, ok := cfg.Remotes["origin"]
	if !ok {
		_, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{url}})
		return err
	}
	if len(rc.URLs) > 0 && rc.URLs[0] == url {
		return nil // já limpo
	}
	rc.URLs = []string{url}
	return repo.SetConfig(cfg)
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
		repo, err := g.openRepo()
		if err != nil {
			return fmt.Errorf("open repo: %w", err)
		}
		// Higiene de credencial: clones antigos podem ter PAT embutido no remote
		// (.git/config). Migração transparente: se não recebemos token por
		// env/flag mas o remote legado carrega um, extraímos para memória e
		// reescrevemos o remote com a URL limpa — auth passa a ser só por Basic.
		if g.token == "" {
			if tok := g.extractRemoteTokenLocked(repo); tok != "" {
				g.token = tok
				log.Printf("[git] legacy PAT migrated from .git/config to memory (header-only)")
			}
		}
		if g.source != "" {
			if err := g.setRemoteURLLocked(repo, g.source); err != nil {
				return fmt.Errorf("scrub remote url: %w", err)
			}
		}
		if err := g.fetchAndResetLocked(repo); err != nil {
			return err
		}
		_ = g.writeLocalExcludesLocked() // best-effort: nunca trackear SQLite DB
		return g.refreshSHA()
	}
	if !g.IsEmpty() {
		return fmt.Errorf("workspace %q is not a git repo and not empty; refuse to clobber. Move files away or rm -rf and retry", g.workspace)
	}
	if err := os.MkdirAll(g.workspace, 0o755); err != nil {
		return err
	}
	_, err := git.PlainClone(g.workspace, false, &git.CloneOptions{
		URL:           g.source,
		Auth:          g.auth(),
		ReferenceName: branchRefName(g.branch),
		SingleBranch:  false, // mantém refspec padrão → refs/remotes/origin/* resolvíveis
		Tags:          git.NoTags,
	})
	if err != nil {
		return fmt.Errorf("git clone: %w", err)
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
	repo, err := g.openRepo()
	if err != nil {
		return err
	}
	if err := g.fetchAndResetLocked(repo); err != nil {
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

func (g *GitOps) fetchAndResetLocked(repo *git.Repository) error {
	if err := g.fetchLocked(repo, g.branch); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	target, err := g.refHash(repo, remoteRefName(g.branch))
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", g.branch, err)
	}
	wt, err := g.worktree(repo)
	if err != nil {
		return err
	}
	if err := g.hardResetTo(repo, wt, target); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	g.lastSync = time.Now()
	return nil
}

func (g *GitOps) refreshSHA() error {
	repo, err := g.openRepo()
	if err != nil {
		return err
	}
	head, err := repo.Head()
	if err != nil {
		return err
	}
	g.lastSHA = head.Hash().String()
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
	// nil-check ANTES do Lock: com receiver nil, g.mu.Lock() panicaria antes
	// do guard (o server sobe com GitOps nil quando não há -git-source).
	if g == nil {
		return Status{Configured: false}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.source == "" {
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

// CheckDrift does a lightweight fetch + compares local HEAD vs origin/<branch>.
// Updates internal drift fields. Safe for polling.
func (g *GitOps) CheckDrift() (drifted bool, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.IsRepo() {
		return false, fmt.Errorf("not a git repo")
	}
	repo, err := g.openRepo()
	if err != nil {
		return false, err
	}
	if err := g.fetchLocked(repo, g.branch); err != nil {
		g.driftErr = err.Error()
		return false, err
	}
	remote, err := g.refHash(repo, remoteRefName(g.branch))
	if err != nil {
		g.driftErr = err.Error()
		return false, err
	}
	head, err := repo.Head()
	if err != nil {
		g.driftErr = err.Error()
		return false, err
	}
	g.remoteSHA = remote.String()
	g.driftErr = ""
	g.drift = remote != head.Hash()
	return g.drift, nil
}

// === F13.2 — write-side primitives (branch + commit + push) ===

// CheckoutNewBranch cria branch a partir de origin/<branch> e checa out.
// Preserva arquivos untracked (não usa o Checkout do go-git, que apaga untracked).
func (g *GitOps) CheckoutNewBranch(branchName string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.IsRepo() {
		return fmt.Errorf("not a git repo")
	}
	repo, err := g.openRepo()
	if err != nil {
		return err
	}
	if err := g.fetchLocked(repo, g.branch); err != nil {
		return err
	}
	target, err := g.refHash(repo, remoteRefName(g.branch))
	if err != nil {
		return err
	}
	ref := branchRefName(branchName)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(ref, target)); err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, ref)); err != nil {
		return err
	}
	wt, err := g.worktree(repo)
	if err != nil {
		return err
	}
	return g.hardResetTo(repo, wt, target)
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
	repo, err := g.openRepo()
	if err != nil {
		return err
	}
	head, err := repo.Head()
	if err != nil {
		return err
	}
	ref := branchRefName(branchName)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(ref, head.Hash())); err != nil {
		return err
	}
	// Só move HEAD para a nova branch; índice + working tree intactos.
	return repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, ref))
}

// HeadSHA retorna o sha do HEAD local atual (sem fetch).
func (g *GitOps) HeadSHA() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	repo, err := g.openRepo()
	if err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}

// IsClean retorna true se o working tree não tem mudanças (status limpo).
// Honra .gitignore (go-git's Status já consulta os padrões de ignore).
// P4 (2026-04-26) — usado para detectar empty publish.
func (g *GitOps) IsClean() (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	repo, err := g.openRepo()
	if err != nil {
		return false, err
	}
	wt, err := g.worktree(repo)
	if err != nil {
		return false, err
	}
	st, err := wt.Status()
	if err != nil {
		return false, err
	}
	return st.IsClean(), nil
}

// AheadBehind retorna (ahead, behind) entre HEAD local e origin/<branch>.
// Faz fetch antes da contagem (assim a info reflete o estado real do remote no
// momento da chamada). Equivale a `rev-list --left-right --count HEAD...origin/<branch>`.
//
// P8 (2026-04-26) — badge ahead/behind por design session.
func (g *GitOps) AheadBehind() (ahead, behind int, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.IsRepo() {
		return 0, 0, fmt.Errorf("not a git repo")
	}
	repo, err := g.openRepo()
	if err != nil {
		return 0, 0, err
	}
	if err := g.fetchLocked(repo, g.branch); err != nil {
		return 0, 0, err
	}
	remote, err := g.refHash(repo, remoteRefName(g.branch))
	if err != nil {
		return 0, 0, err
	}
	head, err := repo.Head()
	if err != nil {
		return 0, 0, err
	}
	return aheadBehind(repo, head.Hash(), remote)
}

func reachableSet(repo *git.Repository, start plumbing.Hash) (map[plumbing.Hash]struct{}, error) {
	seen := map[plumbing.Hash]struct{}{}
	stack := []plumbing.Hash{start}
	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		c, err := repo.CommitObject(h)
		if err != nil {
			return nil, err
		}
		stack = append(stack, c.ParentHashes...)
	}
	return seen, nil
}

func aheadBehind(repo *git.Repository, head, remote plumbing.Hash) (int, int, error) {
	hs, err := reachableSet(repo, head)
	if err != nil {
		return 0, 0, err
	}
	rs, err := reachableSet(repo, remote)
	if err != nil {
		return 0, 0, err
	}
	ahead, behind := 0, 0
	for h := range hs {
		if _, ok := rs[h]; !ok {
			ahead++
		}
	}
	for h := range rs {
		if _, ok := hs[h]; !ok {
			behind++
		}
	}
	return ahead, behind, nil
}

// AddAndCommit estagia todo o working tree (add, modify, delete — honrando
// .gitignore/.git/info/exclude) e commita com message + author. authorName/email
// opcional; se vazio usa "regente <bot@regente.local>".
//
// `path` é mantido por compat de assinatura; todos os call sites estagiam a raiz
// do workspace (".") — o staging é sempre da árvore inteira, como `git add -A .`.
func (g *GitOps) AddAndCommit(path, message, authorName, authorEmail string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	_ = path
	if authorName == "" {
		authorName = "regente"
	}
	if authorEmail == "" {
		authorEmail = "bot@regente.local"
	}
	repo, err := g.openRepo()
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}
	wt, err := g.worktree(repo)
	if err != nil {
		return err
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	sig := &object.Signature{Name: authorName, Email: authorEmail, When: time.Now()}
	if _, err := wt.Commit(message, &git.CommitOptions{
		Author:            sig,
		Committer:         sig,
		AllowEmptyCommits: true,
	}); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// PushBranch faz push da branch local pro origin.
func (g *GitOps) PushBranch(branchName string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	repo, err := g.openRepo()
	if err != nil {
		return err
	}
	return g.pushLocked(repo, branchName)
}

func (g *GitOps) pushLocked(repo *git.Repository, branchName string) error {
	refspec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branchName, branchName))
	err := repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refspec},
		Auth:       g.auth(),
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// RebaseBranch reaplica os commits da branch atual sobre origin/<base> (F13.4
// conflict handling). go-git não tem rebase nativo; implementamos replay linear
// (cherry-pick de cada commit desde a merge-base). NÃO faz merge 3-vias: um
// conflito de conteúdo real é resolvido last-writer-wins, não aborta. Isso é
// aceitável porque o único chamador (PR push-retry) usa nomes de branch únicos
// por timestamp — o caminho de conflito é praticamente inalcançável. Em caso de
// erro o caller chama AbortRebase e desiste.
func (g *GitOps) RebaseBranch(base string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	repo, err := g.openRepo()
	if err != nil {
		return err
	}
	if err := g.fetchLocked(repo, base); err != nil {
		return fmt.Errorf("fetch for rebase: %w", err)
	}
	newBase, err := g.refHash(repo, remoteRefName(base))
	if err != nil {
		return fmt.Errorf("rebase: resolve origin/%s: %w", base, err)
	}
	headRef, err := repo.Head()
	if err != nil {
		return err
	}
	head := headRef.Hash()
	headC, err := repo.CommitObject(head)
	if err != nil {
		return err
	}
	baseC, err := repo.CommitObject(newBase)
	if err != nil {
		return err
	}
	// Se a branch já contém newBase, não há o que rebasear.
	if anc, _ := baseC.IsAncestor(headC); anc {
		return nil
	}
	commits, err := commitsToReplay(repo, head, newBase)
	if err != nil {
		return fmt.Errorf("rebase: %w", err)
	}
	g.rebaseOrig = head
	g.rebaseRef = headRef.Name()
	wt, err := g.worktree(repo)
	if err != nil {
		return err
	}
	if err := g.hardResetTo(repo, wt, newBase); err != nil {
		return fmt.Errorf("rebase reset to base: %w", err)
	}
	for _, c := range commits {
		if err := g.cherryPickLocked(repo, wt, c); err != nil {
			return fmt.Errorf("rebase replay %s: %w", c.Hash.String()[:7], err)
		}
	}
	g.rebaseOrig = plumbing.ZeroHash
	g.rebaseRef = ""
	return nil
}

// commitsToReplay coleta, oldest-first, os commits alcançáveis de head que NÃO
// estão em newBase (os commits únicos da branch desde a merge-base).
func commitsToReplay(repo *git.Repository, head, newBase plumbing.Hash) ([]*object.Commit, error) {
	baseC, err := repo.CommitObject(newBase)
	if err != nil {
		return nil, err
	}
	var out []*object.Commit
	h := head
	for !h.IsZero() {
		c, err := repo.CommitObject(h)
		if err != nil {
			return nil, err
		}
		anc, err := c.IsAncestor(baseC)
		if err != nil {
			return nil, err
		}
		if anc {
			break // c já está contido em newBase
		}
		out = append(out, c)
		if c.NumParents() == 0 {
			break
		}
		h = c.ParentHashes[0] // first-parent walk (feature branches são lineares)
	}
	// reverse → oldest-first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// cherryPickLocked aplica o diff de (parent → c) sobre o working tree atual e
// cria um novo commit preservando autor/committer/mensagem de c.
func (g *GitOps) cherryPickLocked(repo *git.Repository, wt *git.Worktree, c *object.Commit) error {
	cTree, err := c.Tree()
	if err != nil {
		return err
	}
	if c.NumParents() == 0 {
		if err := cTree.Files().ForEach(func(f *object.File) error {
			return writeWorktreeFile(g.workspace, f)
		}); err != nil {
			return err
		}
	} else {
		parent, err := c.Parent(0)
		if err != nil {
			return err
		}
		pTree, err := parent.Tree()
		if err != nil {
			return err
		}
		changes, err := pTree.Diff(cTree)
		if err != nil {
			return err
		}
		for _, ch := range changes {
			action, err := ch.Action()
			if err != nil {
				return err
			}
			if action == merkletrie.Delete {
				abs := filepath.Join(g.workspace, filepath.FromSlash(ch.From.Name))
				if rerr := os.Remove(abs); rerr != nil && !os.IsNotExist(rerr) {
					return rerr
				}
				continue
			}
			// ch.To.Name carrega o path COMPLETO; o File de ch.Files() só traz o
			// basename — por isso usamos ch.To.Name para o destino em disco.
			_, to, err := ch.Files()
			if err != nil {
				return err
			}
			if to == nil { // entrada não-arquivo (submódulo/symlink) — ignora
				continue
			}
			content, err := to.Contents()
			if err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if ch.To.TreeEntry.Mode == filemode.Executable {
				mode = 0o755
			}
			if err := writeFileAt(g.workspace, ch.To.Name, content, mode); err != nil {
				return err
			}
		}
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return err
	}
	author := c.Author
	committer := c.Committer
	_, err = wt.Commit(c.Message, &git.CommitOptions{
		Author:            &author,
		Committer:         &committer,
		AllowEmptyCommits: true,
	})
	return err
}

// writeWorktreeFile materializa um *object.File (cujo Name é o path completo a
// partir da raiz da árvore) no working tree.
func writeWorktreeFile(root string, f *object.File) error {
	content, err := f.Contents()
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if f.Mode == filemode.Executable {
		mode = 0o755
	}
	return writeFileAt(root, f.Name, content, mode)
}

func writeFileAt(root, relPath, content string, mode os.FileMode) error {
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), mode)
}

// AbortRebase restaura a branch + working tree ao estado pré-rebase (F13.4).
// Best-effort: se não há rebase em andamento, no-op.
func (g *GitOps) AbortRebase() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.rebaseOrig.IsZero() || g.rebaseRef == "" {
		return nil
	}
	repo, err := g.openRepo()
	if err != nil {
		return err
	}
	wt, err := g.worktree(repo)
	if err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(g.rebaseRef, g.rebaseOrig)); err != nil {
		return err
	}
	err = g.hardResetTo(repo, wt, g.rebaseOrig)
	g.rebaseOrig = plumbing.ZeroHash
	g.rebaseRef = ""
	return err
}

// CheckoutMain volta para a branch principal e reseta ao remote (descarta
// qualquer mudança local da branch anterior). Preserva untracked.
func (g *GitOps) CheckoutMain() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	repo, err := g.openRepo()
	if err != nil {
		return err
	}
	target, err := g.refHash(repo, remoteRefName(g.branch))
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", g.branch, err)
	}
	ref := branchRefName(g.branch)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(ref, target)); err != nil {
		return fmt.Errorf("checkout main: %w", err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, ref)); err != nil {
		return fmt.Errorf("checkout main: %w", err)
	}
	wt, err := g.worktree(repo)
	if err != nil {
		return err
	}
	if err := g.hardResetTo(repo, wt, target); err != nil {
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
