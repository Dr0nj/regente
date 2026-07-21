// Package storage — Design sessions (Etapa 3+4+5 do realinhamento de produto, 2026-04-26).
//
// Conceito (ver memory/core/regente-product-model.md):
//   - Quando o usuário entra em Design, escolhe quais folders vai mexer.
//   - O servidor cria uma "session" = clone fresco do repo em sessions/<sid>/.
//   - Toda edição (save/delete/createFolder) durante a sessão grava NESSE clone,
//     SEM commit/push automático (Etapa 4 — write local até publicar).
//   - O usuário clica "Publish" → commit + push (modo direct) ou PR
//     (se newFolders > 0, sempre PR — Etapa 5).
//   - A daily continua lendo do workspace principal (./workspace) — sessions
//     são totalmente ortogonais (Opção B já entregue na Etapa 2).
//
// Persistência/ciclo de vida (P3/P6/P7 + recuperação de draft 2026-07-02):
//   - sessions persistem em DB (design_sessions) + clone no disco; Restore() no boot;
//   - GC por TTL remove só sessions LIMPAS — Dirty() (trabalho não publicado) é
//     imune a remoção automática (GC e sweepCleanIdle);
//   - Create varre sessions limpas idle do actor (auto-descarte de esquecidas);
//   - 2 abas no mesmo browser: BroadcastChannel no front (P7, last-claim-wins).
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
)

// DesignSession representa um workspace de edição efêmero.
type DesignSession struct {
	ID         string     `json:"id"`
	Actor      string     `json:"actor"`
	Folders    []string   `json:"folders"`              // folders existentes que o usuário abriu
	NewFolders []string   `json:"newFolders,omitempty"` // folders criados durante a sessão (força PR)
	BaseSHA    string     `json:"baseSha"`              // sha do HEAD no momento do clone
	CreatedAt  time.Time  `json:"createdAt"`
	LastTouch  time.Time  `json:"lastTouch"`
	Path       string     `json:"-"` // sessions/<sid>/
	Store      *FileStore `json:"-"`
	Git        *GitOps    `json:"-"`
}

// SessionManager gerencia o ciclo de vida das design sessions.
type SessionManager struct {
	root   string // ex: ./sessions
	source string // git source (com ou sem token embedded)
	branch string
	token  string // PAT separado (para injetar em cada GitOps novo)
	gh     *GitHubClient
	db     *db.DB // P6 (2026-04-26) — persistência opcional; nil = in-memory only

	mu    sync.Mutex
	items map[string]*DesignSession

	// P3 (2026-04-26) — TTL/GC. ttl=0 desliga GC.
	ttl       time.Duration
	gcTick    time.Duration
	gcStop    chan struct{}
	gcStopped chan struct{}
}

// NewSessionManager — token é opcional; sem token, publish em PR mode falha.
// db é opcional; se != nil, sessions são persistidas em design_sessions e
// restauradas no boot via Restore().
func NewSessionManager(root, source, branch, token string, gh *GitHubClient, db *db.DB) *SessionManager {
	return &SessionManager{
		root:   root,
		source: source,
		branch: branch,
		token:  token,
		gh:     gh,
		db:     db,
		items:  make(map[string]*DesignSession),
	}
}

// SetToken atualiza o PAT usado para clonar novas design sessions (token via UI).
func (m *SessionManager) SetToken(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = strings.TrimSpace(token)
}

// Restore lê design_sessions do DB e recria items para sessions cujo Path
// ainda existe no disco. Sessions sem disco são removidas do DB.
// Idempotente. Sem-op se db==nil.
//
// P6 (2026-04-26).
func (m *SessionManager) Restore() error {
	if m.db == nil {
		return nil
	}
	rows, err := m.db.Query(`SELECT id, actor, folders_json, new_folders_json, base_sha, path, created_at, last_touch FROM design_sessions`)
	if err != nil {
		return fmt.Errorf("design_sessions query: %w", err)
	}
	defer rows.Close()

	restored, dropped := 0, 0
	for rows.Next() {
		var sid, actor, foldersJSON, newFoldersJSON, baseSHA, path string
		var createdAt, lastTouch time.Time
		if err := rows.Scan(&sid, &actor, &foldersJSON, &newFoldersJSON, &baseSHA, &path, &createdAt, &lastTouch); err != nil {
			log.Printf("[design-restore] scan error: %v", err)
			continue
		}
		// Verifica disco — sem .git, é lixo.
		if _, statErr := os.Stat(filepath.Join(path, ".git")); statErr != nil {
			log.Printf("[design-restore] dropping session=%s (path missing: %s)", sid, path)
			if _, delErr := m.db.Exec(`DELETE FROM design_sessions WHERE id=?`, sid); delErr != nil {
				log.Printf("[design-restore] db delete %s failed: %v", sid, delErr)
			}
			dropped++
			continue
		}
		var folders, newFolders []string
		_ = json.Unmarshal([]byte(foldersJSON), &folders)
		_ = json.Unmarshal([]byte(newFoldersJSON), &newFolders)

		git := NewGitOps(path, m.source, m.branch)
		if m.token != "" {
			git.InjectToken(m.token)
		}
		store := NewFileStore(path, false)

		sess := &DesignSession{
			ID: sid, Actor: actor,
			Folders: folders, NewFolders: newFolders,
			BaseSHA:   baseSHA,
			CreatedAt: createdAt, LastTouch: lastTouch,
			Path: path, Store: store, Git: git,
		}
		m.mu.Lock()
		m.items[sid] = sess
		m.mu.Unlock()
		restored++
	}
	log.Printf("[design-restore] restored=%d dropped=%d", restored, dropped)
	return nil
}

// persist grava/atualiza a session no DB. Sem-op se db==nil. Erros logados.
func (m *SessionManager) persist(s *DesignSession) {
	if m.db == nil || s == nil {
		return
	}
	foldersJSON, _ := json.Marshal(s.Folders)
	newFoldersJSON, _ := json.Marshal(s.NewFolders)
	_, err := m.db.Exec(
		`INSERT INTO design_sessions (id, actor, folders_json, new_folders_json, base_sha, path, created_at, last_touch)
		 VALUES (?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   folders_json=excluded.folders_json,
		   new_folders_json=excluded.new_folders_json,
		   last_touch=excluded.last_touch`,
		s.ID, s.Actor, string(foldersJSON), string(newFoldersJSON), s.BaseSHA, s.Path, s.CreatedAt, s.LastTouch,
	)
	if err != nil {
		log.Printf("[design-persist] %s failed: %v", s.ID, err)
	}
}

// PersistSession é wrapper público de persist (callers fora do pacote precisam
// após `AddNewFolder` etc.). P6 (2026-04-26).
func (m *SessionManager) PersistSession(s *DesignSession) { m.persist(s) }

// removePersisted apaga a row do DB. Sem-op se db==nil.
func (m *SessionManager) removePersisted(sid string) {
	if m.db == nil {
		return
	}
	if _, err := m.db.Exec(`DELETE FROM design_sessions WHERE id=?`, sid); err != nil {
		log.Printf("[design-persist] delete %s failed: %v", sid, err)
	}
}

// StartGC dispara goroutine que a cada `tick` remove sessions cujo LastTouch
// é mais antigo que `ttl`. Idempotente: chamada extra é noop.
// ttl<=0 OU tick<=0 desligam GC.
//
// P3 (2026-04-26).
func (m *SessionManager) StartGC(ttl, tick time.Duration) {
	if ttl <= 0 || tick <= 0 {
		log.Printf("[design-gc] disabled (ttl=%v tick=%v)", ttl, tick)
		return
	}
	m.mu.Lock()
	if m.gcStop != nil {
		m.mu.Unlock()
		return // já rodando
	}
	m.ttl = ttl
	m.gcTick = tick
	m.gcStop = make(chan struct{})
	m.gcStopped = make(chan struct{})
	stop := m.gcStop
	stopped := m.gcStopped
	m.mu.Unlock()

	log.Printf("[design-gc] enabled (ttl=%v tick=%v)", ttl, tick)
	go func() {
		defer close(stopped)
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				m.gcSweep()
			}
		}
	}()
}

// StopGC para a goroutine e aguarda terminar.
func (m *SessionManager) StopGC() {
	m.mu.Lock()
	stop, stopped := m.gcStop, m.gcStopped
	m.gcStop, m.gcStopped = nil, nil
	m.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	if stopped != nil {
		<-stopped
	}
}

// gcSweep — coleta sessions expiradas e as deleta (fora do lock).
// Sessions SUJAS (trabalho não publicado) nunca são coletadas: expirar uma
// session limpa custa um re-clone; expirar uma suja perde trabalho humano.
// Elas ficam vivas até o dono retomar/descartar (UI oferece os dois).
func (m *SessionManager) gcSweep() {
	now := time.Now()
	m.mu.Lock()
	ttl := m.ttl
	expired := make([]*DesignSession, 0)
	for _, s := range m.items {
		if now.Sub(s.LastTouch) > ttl {
			expired = append(expired, s)
		}
	}
	m.mu.Unlock()
	for _, s := range expired {
		idle := now.Sub(s.LastTouch)
		if s.Dirty() {
			log.Printf("[design-gc] keeping dirty session=%s idle=%s (unpublished work)", s.ID, idle.Round(time.Second))
			continue
		}
		if err := m.Delete(s.ID); err == nil {
			log.Printf("[design-gc] removed session=%s idle=%s", s.ID, idle.Round(time.Second))
		}
	}
}

// sweepCleanIdle — auto-descarte: remove sessions LIMPAS do actor que estão
// idle há mais que `minIdle`. Chamado no Create pra não acumular clones órfãos
// de sessions esquecidas (F5 sem retomar, aba fechada etc.). O minIdle protege
// sessions limpas em uso ativo em outra aba — o polling de status (30s) e
// qualquer list/save tocam LastTouch via Get. Sujas nunca são removidas.
func (m *SessionManager) sweepCleanIdle(actor string, minIdle time.Duration) {
	now := time.Now()
	m.mu.Lock()
	candidates := make([]*DesignSession, 0)
	for _, s := range m.items {
		if s.Actor == actor && now.Sub(s.LastTouch) > minIdle {
			candidates = append(candidates, s)
		}
	}
	m.mu.Unlock()
	for _, s := range candidates {
		if s.Dirty() {
			continue
		}
		if err := m.Delete(s.ID); err == nil {
			log.Printf("[design-sweep] removed clean session=%s actor=%s idle=%s", s.ID, actor, now.Sub(s.LastTouch).Round(time.Second))
		}
	}
}

// Create clona um workspace fresco e abre uma session.
//
// folders[] = folders existentes que o usuário declarou que vai mexer.
// newFolders[] = folders novos a serem criados (força PR no publish, Etapa 5).
//
// O clone é completo (não filtra por folder) — filtragem é responsabilidade
// do frontend. Backend só garante que sessions não interferem entre si nem
// na daily.
func (m *SessionManager) Create(actor string, folders, newFolders []string) (*DesignSession, error) {
	if m.source == "" {
		return nil, fmt.Errorf("git source not configured; design sessions require GitOps")
	}
	if actor == "" {
		actor = "anon"
	}
	// Auto-descarte: sessions limpas esquecidas do mesmo actor não sobrevivem à
	// criação de uma nova (10min de idle = ninguém está usando; ver sweepCleanIdle).
	m.sweepCleanIdle(actor, 10*time.Minute)
	sid := newSessionID(actor)
	sessionPath := filepath.Join(m.root, sid)
	if err := os.MkdirAll(sessionPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir session: %w", err)
	}

	git := NewGitOps(sessionPath, m.source, m.branch)
	if m.token != "" {
		git.InjectToken(m.token)
	}
	if err := git.EnsureClone(); err != nil {
		_ = os.RemoveAll(sessionPath)
		return nil, fmt.Errorf("clone session workspace: %w", err)
	}

	st := git.Status()
	store := NewFileStore(sessionPath, false) // sessions never auto-commit (Etapa 4)

	sess := &DesignSession{
		ID:         sid,
		Actor:      actor,
		Folders:    append([]string(nil), folders...),
		NewFolders: append([]string(nil), newFolders...),
		BaseSHA:    st.SHA,
		CreatedAt:  time.Now(),
		LastTouch:  time.Now(),
		Path:       sessionPath,
		Store:      store,
		Git:        git,
	}

	// Pré-cria folders novos no disco do session (Etapa 5 — eles existem no
	// session workspace antes de qualquer save; publish detectará e forçará PR).
	for _, nf := range newFolders {
		if err := store.CreateFolder(nf); err != nil {
			log.Printf("[session %s] pre-create newFolder %q failed: %v", sid, nf, err)
		}
	}

	m.mu.Lock()
	m.items[sid] = sess
	m.mu.Unlock()
	m.persist(sess) // P6

	log.Printf("[session %s] created actor=%s base=%s folders=%v new=%v", sid, actor, short7(st.SHA), folders, newFolders)
	return sess, nil
}

// Get retorna a session ou (nil,false).
func (m *SessionManager) Get(sid string) (*DesignSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.items[sid]
	if ok {
		s.LastTouch = time.Now()
	}
	return s, ok
}

// List todas as sessions ativas (ou só do actor se filterActor != "").
func (m *SessionManager) List(filterActor string) []*DesignSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*DesignSession, 0, len(m.items))
	for _, s := range m.items {
		if filterActor == "" || s.Actor == filterActor {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Delete remove a session do mapa e apaga o diretório do disco.
func (m *SessionManager) Delete(sid string) error {
	m.mu.Lock()
	sess, ok := m.items[sid]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session not found")
	}
	delete(m.items, sid)
	m.mu.Unlock()
	m.removePersisted(sid) // P6
	if err := os.RemoveAll(sess.Path); err != nil {
		log.Printf("[session %s] cleanup warning: %v", sid, err)
	}
	log.Printf("[session %s] deleted", sid)
	return nil
}

// Dirty informa se a session tem trabalho não publicado (working tree suja).
// Em erro de leitura do repo, assume suja — o custo de proteger uma session
// vazia é um clone órfão; o custo de descartar uma suja é trabalho perdido.
func (s *DesignSession) Dirty() bool {
	clean, err := s.Git.IsClean()
	if err != nil {
		return true
	}
	return !clean
}

// AddNewFolder registra que um folder novo foi criado durante a sessão.
// Idempotente.
func (s *DesignSession) AddNewFolder(name string) {
	for _, n := range s.NewFolders {
		if n == name {
			return
		}
	}
	s.NewFolders = append(s.NewFolders, name)
	s.LastTouch = time.Now()
}

// AddFolder registra que um folder existente foi aberto na sessão.
// Idempotente. Não força PR. Re-alinhamento Design Fase 2 (2026-04-27).
func (s *DesignSession) AddFolder(name string) {
	for _, n := range s.Folders {
		if n == name {
			return
		}
	}
	s.Folders = append(s.Folders, name)
	s.LastTouch = time.Now()
}

// PublishResult — retorno de Publish.
type PublishResult struct {
	Mode      string `json:"mode"` // "direct" | "pr-required"
	CommitSHA string `json:"commitSha,omitempty"`
	Branch    string `json:"branch,omitempty"`
	PRNumber  int    `json:"prNumber,omitempty"`
	PRURL     string `json:"prUrl,omitempty"`
	Forced    bool   `json:"forcedPR,omitempty"` // true se newFolders forçou PR
}

// Publish — commit + push (modo direct) OU branch+commit+push+PR (modo pr-required).
//
// Se a session tem newFolders, FORÇA modo PR independentemente do writeMode
// global (Etapa 5 — nova folder = sempre PR).
//
// Após sucesso, a session é fechada (caller deve chamar Delete).
func (m *SessionManager) Publish(sid, commitMsg string, writeMode WriteMode) (*PublishResult, error) {
	sess, ok := m.Get(sid)
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	actor := sess.Actor
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("regente: design publish by %s (session %s)", actor, sid)
	}

	// 2026-04-27 — política antiga "newFolders força PR" removida.
	// Agora respeitamos `writeMode` configurado no server. Folders novas e seus
	// jobs vão juntos no mesmo commit/push (direct) ou PR (pr-required).
	forcePR := false
	useMode := writeMode

	// P4 (2026-04-26) — empty publish detection: se nada mudou, retorna noop
	// SEM commit/push/PR. Caller (handler HTTP) ainda pode optar por deletar a session.
	if clean, err := sess.Git.IsClean(); err == nil && clean {
		log.Printf("[session %s] publish noop (working tree clean)", sid)
		return &PublishResult{Mode: "noop"}, nil
	}

	if useMode == WriteModePRRequired {
		if m.gh == nil || !m.gh.HasToken() {
			return nil, fmt.Errorf("publish requires PR (newFolders=%v) but github token not configured", sess.NewFolders)
		}
		branch := fmt.Sprintf("regente/design/%s/%s", SafeBranchName(actor), sid)
		// Cria branch a partir do HEAD local SEM fetch+reset — preserva
		// edições do session no working tree.
		if err := sess.Git.CheckoutLocalBranch(branch); err != nil {
			return nil, fmt.Errorf("checkout branch: %w", err)
		}
		title := fmt.Sprintf("Design publish: %s (session %s)", actor, sid)
		body := fmt.Sprintf("Automated PR by Regente (Design session %s).\nActor: `%s`\nFolders: %v\nNew folders: %v\n", sid, actor, sess.Folders, sess.NewFolders)
		if err := sess.Git.AddAndCommit(".", commitMsg, actor, actor+"@regente.local"); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}
		if err := sess.Git.PushBranch(branch); err != nil {
			return nil, fmt.Errorf("push branch: %w", err)
		}
		commitSHA, _ := sess.Git.HeadSHA()
		pr, err := m.gh.CreatePR(branch, m.branch, title, body)
		if err != nil {
			return &PublishResult{Mode: string(WriteModePRRequired), Branch: branch, CommitSHA: commitSHA, Forced: forcePR}, fmt.Errorf("create PR: %w", err)
		}
		log.Printf("[session %s] published as PR #%d (%s)", sid, pr.Number, pr.HTMLURL)
		return &PublishResult{
			Mode:      string(WriteModePRRequired),
			Branch:    branch,
			CommitSHA: commitSHA,
			PRNumber:  pr.Number,
			PRURL:     pr.HTMLURL,
			Forced:    forcePR,
		}, nil
	}

	// direct mode — commit + push pra main.
	if err := sess.Git.DirectPush(commitMsg, actor); err != nil {
		return nil, fmt.Errorf("direct push: %w", err)
	}
	sha, _ := sess.Git.HeadSHA()
	log.Printf("[session %s] published direct → %s", sid, short7(sha))
	return &PublishResult{
		Mode:      string(WriteModeDirect),
		CommitSHA: sha,
	}, nil
}

func newSessionID(actor string) string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%s-%d-%s", SafeBranchName(actor), time.Now().Unix(), hex.EncodeToString(buf[:]))
}

func short7(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
