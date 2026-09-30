package storage

import (
	"github.com/Dr0nj/regente-server/internal/domain"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"testing"
	"time"
)

func TestI07DefinitionSnapshotUsesCommit(t *testing.T) {
	dir := t.TempDir()
	repo, e := git.PlainInit(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	wt, e := repo.Worktree()
	if e != nil {
		t.Fatal(e)
	}
	store := NewFileStore(dir, false)
	def := domain.JobDefinition{ID: "job", Team: "T", Label: "A"}
	if e = store.Save(def); e != nil {
		t.Fatal(e)
	}
	if _, e = wt.Add("definitions/T/job.yaml"); e != nil {
		t.Fatal(e)
	}
	sha, e := wt.Commit("synthetic source A", &git.CommitOptions{Author: &object.Signature{Name: "fixture", Email: "fixture@example.invalid", When: time.Now()}})
	if e != nil {
		t.Fatal(e)
	}
	def.Label = "B"
	if e = store.Save(def); e != nil {
		t.Fatal(e)
	}
	g := NewGitOps(dir, "", "main")
	defs, head, e := g.DefinitionSnapshot()
	if e != nil || head != sha.String() || len(defs) != 1 || defs[0].Label != "A" {
		t.Fatal("misturou worktree com commit", defs, head, e)
	}
	if e = g.AddAndCommit("definitions/T/job.yaml", "synthetic source B", "fixture", "fixture@example.invalid"); e != nil {
		t.Fatal(e)
	}
	defs, newHead, e := g.DefinitionSnapshot()
	if e != nil || newHead == head || defs[0].Label != "B" {
		t.Fatal(defs, newHead, e)
	}
}

func TestI07DefinitionSnapshotConcurrentPublication(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(dir, false)
	def := domain.JobDefinition{ID: "job", Team: "T", Label: "A"}
	if err = store.Save(def); err != nil {
		t.Fatal(err)
	}
	if _, err = wt.Add("definitions/T/job.yaml"); err != nil {
		t.Fatal(err)
	}
	headA, err := wt.Commit("source A", &git.CommitOptions{Author: &object.Signature{Name: "fixture", Email: "fixture@example.invalid", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	def.Label = "B"
	if err = store.Save(def); err != nil {
		t.Fatal(err)
	}
	g := NewGitOps(dir, "", "main")
	done := make(chan error, 1)
	go func() {
		done <- g.AddAndCommit("definitions/T/job.yaml", "source B", "fixture", "fixture@example.invalid")
	}()
	for i := 0; i < 20; i++ {
		defs, sha, err := g.DefinitionSnapshot()
		if err != nil || len(defs) != 1 {
			t.Fatal(defs, err)
		}
		want := "B"
		if sha == headA.String() {
			want = "A"
		}
		if defs[0].Label != want {
			t.Fatalf("publicação misturou SHA %s e payload %s", sha, defs[0].Label)
		}
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
