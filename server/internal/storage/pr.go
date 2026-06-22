// Package storage — write-side orchestrator (F13.2).
//
// Suporta 3 modos de escrita:
//
//   - "direct"      : grava direto em disco/main (legado; útil sem GitHub).
//   - "pr-required" : sempre cria branch + commit + push + PR (default novo recomendado).
//   - "pr-optional" : caller decide via flag opcional.
//
// O orquestrador NUNCA toca a branch principal local enquanto está em modo PR:
//  1. CheckoutNewBranch (a partir de origin/main fresco)
//  2. Caller aplica mudança em disco (Save/Delete via FileStore)
//  3. AddAndCommit
//  4. PushBranch
//  5. CreatePR via GitHub API
//  6. CheckoutMain (volta + reset --hard, deixa workspace coerente com remote)
package storage

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
)

type WriteMode string

const (
	WriteModeDirect     WriteMode = "direct"
	WriteModePRRequired WriteMode = "pr-required"
	WriteModePROptional WriteMode = "pr-optional"
)

func ParseWriteMode(s string) WriteMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pr", "pr-required":
		return WriteModePRRequired
	case "pr-optional":
		return WriteModePROptional
	case "", "direct":
		return WriteModeDirect
	default:
		return WriteModeDirect
	}
}

type PRWriter struct {
	Git    *GitOps
	GH     *GitHubClient
	Store  *FileStore
	Mode   WriteMode
	Author string // default actor when caller doesn't pass user
}

// PRResult é retornado por SaveWithPR/DeleteWithPR.
type PRResult struct {
	PRNumber  int    `json:"prNumber,omitempty"`
	PRURL     string `json:"prUrl,omitempty"`
	Branch    string `json:"branch,omitempty"`
	CommitSHA string `json:"commitSha,omitempty"`
	Mode      string `json:"mode"`
}

// Apply executa o ciclo completo PR:
//
//	prepare branch -> mutate (callback) -> commit -> push -> create PR -> reset to main.
//
// `mutate` é chamado com a workspace já no estado da nova branch.
// Caller usa esse callback para invocar FileStore.Save/Delete/etc.
//
// Se a operação inteira falhar, tenta `CheckoutMain` para deixar workspace consistente.
func (w *PRWriter) Apply(actor, branchSuffix, commitMsg, prTitle, prBody string, mutate func() error) (*PRResult, error) {
	if w == nil || w.Git == nil {
		return nil, fmt.Errorf("PRWriter not configured")
	}
	if w.GH == nil || !w.GH.HasToken() {
		return nil, fmt.Errorf("github token not configured (GITHUB_TOKEN)")
	}
	if actor == "" {
		actor = w.Author
	}
	if actor == "" {
		actor = "regente"
	}
	branch := fmt.Sprintf("regente/%s/%d-%s",
		SafeBranchName(actor),
		time.Now().Unix(),
		SafeBranchName(branchSuffix),
	)

	// 1. Cria branch a partir de origin/main fresco.
	if err := w.Git.CheckoutNewBranch(branch); err != nil {
		_ = w.Git.CheckoutMain()
		return nil, fmt.Errorf("create branch: %w", err)
	}

	// 2. Caller aplica mudança em disco.
	if err := mutate(); err != nil {
		_ = w.Git.CheckoutMain()
		return nil, fmt.Errorf("mutate: %w", err)
	}

	// 3. Commit (tudo em workspace).
	if err := w.Git.AddAndCommit(filepath.Join(w.Store.root), commitMsg, actor, actor+"@regente.local"); err != nil {
		_ = w.Git.CheckoutMain()
		return nil, fmt.Errorf("commit: %w", err)
	}

	// 4. Push branch — with F13.4 retry+rebase on conflict.
	const maxPushRetries = 3
	var pushErr error
	for attempt := 1; attempt <= maxPushRetries; attempt++ {
		pushErr = w.Git.PushBranch(branch)
		if pushErr == nil {
			break
		}
		// If push failed because remote moved (non-fast-forward), try rebase.
		errMsg := pushErr.Error()
		isConflict := strings.Contains(errMsg, "non-fast-forward") ||
			strings.Contains(errMsg, "fetch first") ||
			strings.Contains(errMsg, "rejected")
		if !isConflict || attempt == maxPushRetries {
			break
		}
		log.Printf("[pr-writer] push attempt %d failed (conflict), rebasing onto origin/%s", attempt, w.Git.Branch())
		if rebaseErr := w.Git.RebaseBranch(w.Git.Branch()); rebaseErr != nil {
			// Rebase failed (true merge conflict) — abort and give up.
			_ = w.Git.AbortRebase()
			pushErr = fmt.Errorf("rebase conflict (manual resolution needed): %w", rebaseErr)
			break
		}
	}
	if pushErr != nil {
		_ = w.Git.CheckoutMain()
		return nil, fmt.Errorf("push: %w", pushErr)
	}

	// commit SHA depois do commit
	commitSHA, _ := w.Git.HeadSHA()

	// 5. Create PR.
	pr, err := w.GH.CreatePR(branch, w.Git.Branch(), prTitle, prBody)
	if err != nil {
		// branch já está no remote — não dá pra "voltar atrás" facilmente.
		_ = w.Git.CheckoutMain()
		return &PRResult{Branch: branch, CommitSHA: commitSHA, Mode: string(WriteModePRRequired)},
			fmt.Errorf("create PR: %w", err)
	}

	// 6. Volta workspace pra main coerente com remote (sem mudanças locais).
	if err := w.Git.CheckoutMain(); err != nil {
		// não fatal; só log para o caller
		return &PRResult{
			PRNumber:  pr.Number,
			PRURL:     pr.HTMLURL,
			Branch:    branch,
			CommitSHA: commitSHA,
			Mode:      string(WriteModePRRequired),
		}, fmt.Errorf("post-PR checkout main warn: %w", err)
	}

	return &PRResult{
		PRNumber:  pr.Number,
		PRURL:     pr.HTMLURL,
		Branch:    branch,
		CommitSHA: commitSHA,
		Mode:      string(WriteModePRRequired),
	}, nil
}
