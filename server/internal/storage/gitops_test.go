package storage

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// These tests exercise the native go-git backend end-to-end against a LOCAL bare
// repo acting as the remote (no network, no `git` binary). The "remote" default
// branch is "master" (go-git PlainInit default), so GitOps is created with that
// branch throughout.

const testBranch = "master"

func sig() *object.Signature { return &object.Signature{Name: "t", Email: "t@t"} }

// initRemote creates a bare repo + a seed working clone with one commit pushed.
// Returns the bare path and a seedRepo helper bound to the seed working dir.
func initRemote(t *testing.T) (bare string, seed *seedRepo) {
	t.Helper()
	bare = t.TempDir()
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	seedDir := t.TempDir()
	r, err := git.PlainInit(seedDir, false)
	if err != nil {
		t.Fatalf("init seed: %v", err)
	}
	if _, err := r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{bare}}); err != nil {
		t.Fatalf("seed remote: %v", err)
	}
	s := &seedRepo{t: t, repo: r, dir: seedDir}
	s.commit("README.md", "hello\n", "initial")
	s.push()
	return bare, s
}

type seedRepo struct {
	t    *testing.T
	repo *git.Repository
	dir  string
}

func (s *seedRepo) commit(path, content, msg string) plumbing.Hash {
	s.t.Helper()
	abs := filepath.Join(s.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
	wt, _ := s.repo.Worktree()
	if _, err := wt.Add(path); err != nil {
		s.t.Fatalf("seed add: %v", err)
	}
	h, err := wt.Commit(msg, &git.CommitOptions{Author: sig(), Committer: sig()})
	if err != nil {
		s.t.Fatalf("seed commit: %v", err)
	}
	return h
}

func (s *seedRepo) push() {
	s.t.Helper()
	err := s.repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec("refs/heads/master:refs/heads/master")},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		s.t.Fatalf("seed push: %v", err)
	}
}

// sync fast-forwards the seed working repo to the bare's master (used after the
// GitOps side has pushed, so the seed can keep committing without divergence).
func (s *seedRepo) sync() {
	s.t.Helper()
	if err := s.repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec("+refs/heads/master:refs/remotes/origin/master")},
		Force:      true,
	}); err != nil && err != git.NoErrAlreadyUpToDate {
		s.t.Fatalf("seed sync fetch: %v", err)
	}
	ref, err := s.repo.Reference(plumbing.NewRemoteReferenceName("origin", "master"), true)
	if err != nil {
		s.t.Fatalf("seed sync ref: %v", err)
	}
	wt, _ := s.repo.Worktree()
	if err := wt.Reset(&git.ResetOptions{Commit: ref.Hash(), Mode: git.HardReset}); err != nil {
		s.t.Fatalf("seed sync reset: %v", err)
	}
}

func (s *seedRepo) headSHA() string {
	s.t.Helper()
	h, err := s.repo.Head()
	if err != nil {
		s.t.Fatal(err)
	}
	return h.Hash().String()
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func TestEnsureCloneFreshAndIdempotent(t *testing.T) {
	bare, seed := initRemote(t)
	ws := t.TempDir()
	// workspace must be empty for fresh clone; t.TempDir() is empty.
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("EnsureClone (fresh): %v", err)
	}
	if !g.IsRepo() {
		t.Fatal("workspace not a repo after clone")
	}
	if got := g.Status().SHA; got != seed.headSHA() {
		t.Fatalf("clone SHA = %s, want %s", got, seed.headSHA())
	}
	if readFile(t, filepath.Join(ws, "README.md")) != "hello\n" {
		t.Fatal("README not materialized")
	}
	// Re-run on an existing repo → fetch+reset path (idempotent).
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("EnsureClone (existing): %v", err)
	}
}

func TestSyncFromRemotePreservesUntracked(t *testing.T) {
	bare, seed := initRemote(t)
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	// Simulate a local SQLite DB sitting in the workspace (untracked) + a stray
	// untracked file. `git reset --hard` must NOT delete these.
	dbPath := filepath.Join(ws, "regente.db")
	strayPath := filepath.Join(ws, "scratch.tmp")
	mustWrite(t, dbPath, "SQLITE")
	mustWrite(t, strayPath, "stray")

	// Advance the remote.
	want := seed.commit("definitions/job.yaml", "name: x\n", "add job")
	seed.push()

	if err := g.SyncFromRemote(); err != nil {
		t.Fatalf("SyncFromRemote: %v", err)
	}
	if got := g.Status().SHA; got != want.String() {
		t.Fatalf("after sync SHA = %s, want %s", got, want.String())
	}
	if !exists(filepath.Join(ws, "definitions/job.yaml")) {
		t.Fatal("new tracked file not pulled")
	}
	if !exists(dbPath) {
		t.Fatal("untracked SQLite DB was deleted by reset --hard (regression)")
	}
	if !exists(strayPath) {
		t.Fatal("untracked stray file was deleted by reset --hard (regression)")
	}
}

func TestSyncDiscardsLocalTrackedEdits(t *testing.T) {
	bare, seed := initRemote(t)
	_ = seed
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	// Locally tamper with a tracked file; reset --hard must restore it.
	mustWrite(t, filepath.Join(ws, "README.md"), "TAMPERED")
	if err := g.SyncFromRemote(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := readFile(t, filepath.Join(ws, "README.md")); got != "hello\n" {
		t.Fatalf("tracked edit not discarded: %q", got)
	}
}

func TestCheckDrift(t *testing.T) {
	bare, seed := initRemote(t)
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	drift, err := g.CheckDrift()
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if drift {
		t.Fatal("unexpected drift right after clone")
	}
	seed.commit("a.txt", "1\n", "remote moves")
	seed.push()
	drift, err = g.CheckDrift()
	if err != nil {
		t.Fatalf("CheckDrift 2: %v", err)
	}
	if !drift {
		t.Fatal("expected drift after remote advanced")
	}
	if g.Status().RemoteSHA != seed.headSHA() {
		t.Fatalf("RemoteSHA = %s, want %s", g.Status().RemoteSHA, seed.headSHA())
	}
}

func TestDirectPushAndAheadBehind(t *testing.T) {
	bare, seed := initRemote(t)
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	// Write a new definition and push directly.
	mustWrite(t, filepath.Join(ws, "definitions/new.yaml"), "name: new\n")
	if err := g.DirectPush("add new", "tester"); err != nil {
		t.Fatalf("DirectPush: %v", err)
	}
	// Seed pulls from bare and should see the file.
	seed.repo.Fetch(&git.FetchOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{config.RefSpec("+refs/heads/master:refs/remotes/origin/master")}})
	ref, err := seed.repo.Reference(plumbing.NewRemoteReferenceName("origin", "master"), true)
	if err != nil {
		t.Fatalf("seed read origin/master: %v", err)
	}
	if ref.Hash().String() != g.Status().SHA {
		t.Fatalf("remote master = %s, want pushed %s", ref.Hash().String(), g.Status().SHA)
	}
	// No divergence right after push.
	ahead, behind, err := g.AheadBehind()
	if err != nil {
		t.Fatalf("AheadBehind: %v", err)
	}
	if ahead != 0 || behind != 0 {
		t.Fatalf("ahead/behind = %d/%d, want 0/0", ahead, behind)
	}
	// Remote advances → we are behind by 1. Seed must catch up to the pushed
	// state first (GitOps just advanced bare/master) to avoid a non-ff push.
	seed.sync()
	seed.commit("z.txt", "z\n", "remote ahead")
	seed.push()
	ahead, behind, err = g.AheadBehind()
	if err != nil {
		t.Fatalf("AheadBehind 2: %v", err)
	}
	if ahead != 0 || behind != 1 {
		t.Fatalf("ahead/behind = %d/%d, want 0/1", ahead, behind)
	}
}

func TestCheckoutNewBranchAndLocalBranch(t *testing.T) {
	bare, seed := initRemote(t)
	_ = seed
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	// CheckoutNewBranch branches from origin/master fresh.
	if err := g.CheckoutNewBranch("regente/feature-1"); err != nil {
		t.Fatalf("CheckoutNewBranch: %v", err)
	}
	// Mutate + commit + push the feature branch.
	mustWrite(t, filepath.Join(ws, "definitions/f.yaml"), "name: f\n")
	if err := g.AddAndCommit(".", "feature commit", "tester", "t@t"); err != nil {
		t.Fatalf("AddAndCommit: %v", err)
	}
	if err := g.PushBranch("regente/feature-1"); err != nil {
		t.Fatalf("PushBranch: %v", err)
	}
	// Back to main; the feature file is gone from the working tree.
	if err := g.CheckoutMain(); err != nil {
		t.Fatalf("CheckoutMain: %v", err)
	}
	if exists(filepath.Join(ws, "definitions/f.yaml")) {
		t.Fatal("feature file should not be present on main after CheckoutMain")
	}
}

func TestCheckoutLocalBranchKeepsWorktree(t *testing.T) {
	bare, _ := initRemote(t)
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	// Edit on disk first (like a design session), THEN branch — edits must survive.
	mustWrite(t, filepath.Join(ws, "definitions/session.yaml"), "name: s\n")
	if err := g.CheckoutLocalBranch("regente/session-1"); err != nil {
		t.Fatalf("CheckoutLocalBranch: %v", err)
	}
	if !exists(filepath.Join(ws, "definitions/session.yaml")) {
		t.Fatal("working tree edit lost on local branch checkout")
	}
	clean, err := g.IsClean()
	if err != nil {
		t.Fatalf("IsClean: %v", err)
	}
	if clean {
		t.Fatal("expected dirty worktree (uncommitted session edit)")
	}
}

func TestRebaseBranchReplaysOntoMovedBase(t *testing.T) {
	bare, seed := initRemote(t)
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	// Create a feature branch with one commit (touches its own file).
	if err := g.CheckoutNewBranch("regente/rebase-me"); err != nil {
		t.Fatalf("CheckoutNewBranch: %v", err)
	}
	mustWrite(t, filepath.Join(ws, "definitions/feature.yaml"), "name: feat\n")
	if err := g.AddAndCommit(".", "feature work", "tester", "t@t"); err != nil {
		t.Fatalf("AddAndCommit: %v", err)
	}
	featSHA, _ := g.HeadSHA()
	// Meanwhile, origin/master advances with an unrelated file.
	seed.commit("definitions/other.yaml", "name: other\n", "base moves")
	seed.push()
	newBase := seed.headSHA()

	// Rebase the feature branch onto the new origin/master.
	if err := g.RebaseBranch(testBranch); err != nil {
		t.Fatalf("RebaseBranch: %v", err)
	}
	headSHA, _ := g.HeadSHA()
	if headSHA == featSHA {
		t.Fatal("rebase did not create a new commit (head unchanged)")
	}
	// The rebased branch must contain BOTH the new base file and the feature file.
	if !exists(filepath.Join(ws, "definitions/other.yaml")) {
		t.Fatal("rebased tree missing the new base file")
	}
	if !exists(filepath.Join(ws, "definitions/feature.yaml")) {
		t.Fatal("rebased tree missing the replayed feature file")
	}
	// New base must be an ancestor of the rebased head.
	repo, _ := g.openRepo()
	headC, _ := repo.CommitObject(plumbing.NewHash(headSHA))
	baseC, _ := repo.CommitObject(plumbing.NewHash(newBase))
	anc, err := baseC.IsAncestor(headC)
	if err != nil {
		t.Fatalf("IsAncestor: %v", err)
	}
	if !anc {
		t.Fatal("new base is not an ancestor of rebased head")
	}
}

func TestCleanupDBArtifactsUntracksAndPushes(t *testing.T) {
	bare, seed := initRemote(t)
	// Pollute the remote: commit a SQLite DB file.
	seed.commit("regente.db", "POLLUTED", "oops committed db")
	seed.push()
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	// Local bare needs no real auth, but the cleanup push is gated on having a
	// credential; inject a dummy token so the push path runs (auth() is nil for
	// non-https sources anyway).
	g.InjectToken("dummy")
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if !exists(filepath.Join(ws, "regente.db")) {
		t.Fatal("polluted db should be present after clone")
	}
	changed, err := g.CleanupDBArtifacts("tester")
	if err != nil {
		t.Fatalf("CleanupDBArtifacts: %v", err)
	}
	if !changed {
		t.Fatal("expected cleanup to report changes")
	}
	// File stays on disk (rm --cached, not rm), but is untracked now.
	if !exists(filepath.Join(ws, "regente.db")) {
		t.Fatal("db file should remain on disk after untrack")
	}
	// HEAD tree must no longer contain regente.db.
	repo, _ := g.openRepo()
	head, _ := repo.Head()
	c, _ := repo.CommitObject(head.Hash())
	tree, _ := c.Tree()
	if _, err := tree.File("regente.db"); err == nil {
		t.Fatal("regente.db still tracked in HEAD tree after cleanup")
	}
	if _, err := tree.File(".gitignore"); err != nil {
		t.Fatal(".gitignore not committed by cleanup")
	}
	// Second run is idempotent (nothing left to clean).
	changed, err = g.CleanupDBArtifacts("tester")
	if err != nil {
		t.Fatalf("CleanupDBArtifacts (2): %v", err)
	}
	if changed {
		t.Fatal("second cleanup should be a no-op")
	}
}

func mustWrite(t *testing.T, abs, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
