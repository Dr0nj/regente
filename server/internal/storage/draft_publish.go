package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type draftPublication struct {
	Mode   WriteMode      `json:"mode"`
	Branch string         `json:"branch"`
	SHA    string         `json:"sha"`
	Actor  string         `json:"actor"`
	Result *PublishResult `json:"result,omitempty"`
}

// RemoteRead é source-driven e nunca reseta os arquivos do draft.
func (g *GitOps) remoteRead(branch string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	repo, err := g.openRepo()
	if err != nil {
		return "", err
	}
	if err = g.fetchLocked(repo, branch); err != nil {
		return "", err
	}
	ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	if err != nil {
		return "", err
	}
	return ref.Hash().String(), nil
}

// Guarda o commit preparado ANTES de qualquer efeito remoto. Retry usa o mesmo
// objeto, inclusive após queda no intervalo entre push e confirmação no banco.
func (m *SessionManager) PublishSession(s *DesignSession, message string, mode WriteMode, actor string) (*PublishResult, error) {
	if m.db == nil {
		return m.Publish(s.ID, message, mode)
	}
	var intent draftPublication
	if s.State == "published" {
		if err := json.Unmarshal([]byte(s.publication), &intent); err != nil {
			return nil, err
		}
		if intent.Result == nil {
			return nil, fmt.Errorf("missing publication receipt")
		}
		return intent.Result, nil
	}
	if s.State == "publishing" {
		if err := json.Unmarshal([]byte(s.publication), &intent); err != nil {
			return nil, err
		}
		head, err := s.Git.HeadSHA()
		if err != nil || head != intent.SHA {
			return nil, fmt.Errorf("publication snapshot differs from prepared commit")
		}
	} else {
		if !s.Dirty() {
			return &PublishResult{Mode: "noop"}, nil
		}
		if mode == WriteModePRRequired && (m.gh == nil || !m.gh.HasToken()) {
			return nil, fmt.Errorf("GitHub token required for pull request publication")
		}
		branch := s.Git.branch
		if mode == WriteModePRRequired {
			branch = fmt.Sprintf("regente/design/%s/%s", SafeBranchName(s.Actor), s.ID)
		} else {
			remote, err := s.Git.remoteRead(branch)
			if err != nil {
				return nil, err
			}
			if remote != s.BaseSHA {
				return nil, fmt.Errorf("%w: Git branch advanced from draft base; preserve/export the draft and review changes before publication", ErrDraftConflict)
			}
		}
		if err := s.Git.CheckoutLocalBranch(branch); err != nil {
			return nil, err
		}
		if message == "" {
			message = fmt.Sprintf("regente: design publish by %s (session %s)", s.Actor, s.ID)
		}
		clean, err := s.Git.IsClean()
		if err != nil {
			return nil, err
		}
		if !clean {
			if err = s.Git.AddAndCommit(".", message, s.Actor, s.Actor+"@regente.local"); err != nil {
				return nil, err
			}
		}
		sha, err := s.Git.HeadSHA()
		if err != nil {
			return nil, err
		}
		intent = draftPublication{Mode: mode, Branch: branch, SHA: sha, Actor: actor}
		raw, _ := json.Marshal(intent)
		s.publication = string(raw)
		s.State = "publishing"
		if err = m.Checkpoint(s, actor, "publish-prepare"); err != nil {
			return nil, err
		}
	}
	// Reconhecer push já concluído, inclusive se o remoto avançou depois dele.
	already := false
	if remote, e := s.Git.remoteRead(intent.Branch); e == nil {
		repo, e := git.PlainOpen(s.Path)
		if e != nil {
			return nil, e
		}
		commit, e := repo.CommitObject(plumbing.NewHash(intent.SHA))
		if e != nil {
			return nil, e
		}
		tip, e := repo.CommitObject(plumbing.NewHash(remote))
		if e != nil {
			return nil, e
		}
		already, e = commit.IsAncestor(tip)
		if e != nil {
			return nil, e
		}
	}
	// Normal push somente. Commit local sem push não é um noop.
	if !already {
		if err := s.Git.PushBranch(intent.Branch); err != nil {
			return nil, fmt.Errorf("publication pending; prepared draft preserved for retry/export: %w", err)
		}
	}
	res := &PublishResult{Mode: string(intent.Mode), Branch: intent.Branch, CommitSHA: intent.SHA}
	if intent.Mode == WriteModePRRequired {
		if m.gh == nil || !m.gh.HasToken() {
			return nil, fmt.Errorf("publication pending: GitHub token required")
		}
		pr, err := m.gh.FindPR(intent.Branch, m.branch)
		if err != nil {
			return nil, err
		}
		if pr == nil {
			pr, err = m.gh.CreatePR(intent.Branch, m.branch, fmt.Sprintf("Design publish: %s (session %s)", s.Actor, s.ID), fmt.Sprintf("Automated PR by Regente (Design session %s).", s.ID))
			if err != nil {
				// Outra tentativa pode ter criado a PR após nosso GET. Resolve por head único.
				found, readErr := m.gh.FindPR(intent.Branch, m.branch)
				if readErr != nil || found == nil {
					return nil, err
				}
				pr = found
			}
		}
		res.PRNumber = pr.Number
		res.PRURL = pr.HTMLURL
	}
	intent.Result = res
	raw, _ := json.Marshal(intent)
	tx, err := m.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE design_sessions SET state='published',publication=? WHERE id=? AND revision=? AND state='publishing'", string(raw), s.ID, s.Revision)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		current, e := m.Metadata(s.ID)
		if e != nil {
			return nil, ErrDraftConflict
		}
		if current.State == "published" {
			return res, nil
		}
		return nil, ErrDraftConflict
	}
	if err = draftAudit(tx, s.ID, s.Revision, actor, "publish", "", s.LastTouch.UnixMilli()); err != nil {
		return nil, err
	}
	receipt, _ := json.Marshal(res)
	folders, _ := json.Marshal(append(append([]string{}, s.Folders...), s.NewFolders...))
	if _, err = tx.Exec("INSERT INTO design_draft_publications(session_id,owner,revision,folders_json,result_json,at_ms) VALUES(?,?,?,?,?,?)", s.ID, s.Actor, s.Revision, string(folders), string(receipt), s.LastTouch.UnixMilli()); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.State = "published"
	s.publication = string(raw)
	return res, nil
}

// Explicitamente liberar a publicação falhada só após provar que o commit não
// está no remoto. Resultado incerto continua protegido e exportável.
func (m *SessionManager) CancelPublication(s *DesignSession, actor string) error {
	if m.db == nil || s.State != "publishing" {
		return ErrDraftConflict
	}
	var intent draftPublication
	if err := json.Unmarshal([]byte(s.publication), &intent); err != nil {
		return err
	}
	remote, err := s.Git.remoteRead(intent.Branch)
	if err != nil {
		return err
	}
	repo, err := git.PlainOpen(s.Path)
	if err != nil {
		return err
	}
	prepared, err := repo.CommitObject(plumbing.NewHash(intent.SHA))
	if err != nil {
		return err
	}
	tip, err := repo.CommitObject(plumbing.NewHash(remote))
	if err != nil {
		return err
	}
	contains, err := prepared.IsAncestor(tip)
	if err != nil {
		return err
	}
	if contains {
		return fmt.Errorf("commit is already published; retry publication to recover its receipt")
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec("UPDATE design_sessions SET state='active',publication='' WHERE id=? AND revision=? AND state='publishing'", s.ID, s.Revision)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrDraftConflict
	}
	if err = draftAudit(tx, s.ID, s.Revision, actor, "publish-cancel", "", s.LastTouch.UnixMilli()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.State = "active"
	s.publication = ""
	return nil
}

func (m *SessionManager) Published(sid string) (*DesignSession, *PublishResult, error) {
	if m.db == nil {
		return nil, nil, ErrDraftNotFound
	}
	s := &DesignSession{ID: sid, State: "published"}
	var folders, result string
	err := m.db.QueryRow("SELECT owner,revision,folders_json,result_json FROM design_draft_publications WHERE session_id=?", sid).Scan(&s.Actor, &s.Revision, &folders, &result)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrDraftNotFound
		}
		return nil, nil, err
	}
	if err = json.Unmarshal([]byte(folders), &s.Folders); err != nil {
		return nil, nil, err
	}
	var res PublishResult
	if err = json.Unmarshal([]byte(result), &res); err != nil {
		return nil, nil, err
	}
	return s, &res, nil
}

// Inclui alterações já commitadas no preparo e working tree; ACL não depende
// apenas das folders que o cliente declarou ter aberto.
func (g *GitOps) ChangedDraftPaths(baseSHA string) ([]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	repo, err := g.openRepo()
	if err != nil {
		return nil, err
	}
	base, err := repo.CommitObject(plumbing.NewHash(baseSHA))
	if err != nil {
		return nil, err
	}
	head, err := repo.Head()
	if err != nil {
		return nil, err
	}
	current, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	oldTree, err := base.Tree()
	if err != nil {
		return nil, err
	}
	newTree, err := current.Tree()
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(oldTree, newTree)
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for _, change := range changes {
		if change.From.Name != "" {
			paths[change.From.Name] = true
		}
		if change.To.Name != "" {
			paths[change.To.Name] = true
		}
	}
	wt, err := g.worktree(repo)
	if err != nil {
		return nil, err
	}
	status, err := wt.Status()
	if err != nil {
		return nil, err
	}
	for path, state := range status {
		if state.Staging != git.Unmodified || state.Worktree != git.Unmodified {
			paths[path] = true
		}
	}
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}
