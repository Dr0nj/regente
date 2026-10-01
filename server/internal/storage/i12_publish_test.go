package storage

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

type i12Transport func(*http.Request) (*http.Response, error)

func (f i12Transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestI12PreparedPRRetryAndGC(t *testing.T) {
	database, err := db.Open(db.SQLite, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err = db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	source, seed := initRemote(t)
	root := t.TempDir()
	gh, err := NewGitHubClient("synthetic-secret-never-persist", "lab/workspace", "")
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	retry := false
	posts := 0
	gh.http = &http.Client{Transport: i12Transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer synthetic-secret-never-persist" {
			t.Error("missing auth")
		}
		status := 200
		body := "[]"
		if r.Method == "POST" {
			posts++
			failed = true
			status = 503
			body = "synthetic lost PR response"
		} else if failed && !retry {
			status = 503
			body = "synthetic lookup outage"
		} else if retry {
			body = `[{"number":42,"html_url":"https://example.invalid/pr/42","state":"open"}]`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	a := NewSessionManager(root, source, testBranch, "synthetic-secret-never-persist", gh, database)
	fresh, err := a.Create("owner", []string{"lab"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	edited, done, err := a.Open(fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if err = edited.Store.Save(domain.JobDefinition{ID: "job", Label: "Preserved", Team: "lab", JobType: "COMMAND", Params: map[string]interface{}{"command": "echo synthetic"}}); err != nil {
		t.Fatal(err)
	}
	if err = a.Checkpoint(edited, "owner", "save"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if _, err = database.Exec("UPDATE design_sessions SET last_touch=? WHERE id=?", old, edited.ID); err != nil {
		t.Fatal(err)
	}
	a.ttl = time.Minute
	a.gcSweep()
	a.sweepCleanIdle("owner", time.Minute)
	if _, err = a.Metadata(edited.ID); err != nil {
		t.Fatal("GC lost shared edit", err)
	}
	if _, err = a.PublishSession(edited, "synthetic PR", WriteModePRRequired, "owner"); err == nil {
		t.Fatal("expected failed PR response")
	}
	meta, err := a.Metadata(edited.ID)
	if err != nil || meta.State != "publishing" || !meta.Dirty() {
		t.Fatal("prepared publication not durable", meta, err)
	}
	preparedSHA, err := edited.Git.HeadSHA()
	if err != nil {
		t.Fatal(err)
	}
	bare, err := git.PlainOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := bare.Reference(plumbing.NewBranchReferenceName("regente/design/owner/"+fresh.ID), true)
	if err != nil || ref.Hash().String() != preparedSHA {
		t.Fatal("prepared branch not pushed", err)
	}
	if err = a.DeleteRevision(meta, "owner"); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("pending publication discarded", err)
	}
	if err = os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	b := NewSessionManager(t.TempDir(), source, testBranch, "synthetic-secret-never-persist", gh, database)
	recovered, cleanup, err := b.Open(fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	exported, err := b.Export(recovered)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := decodeDraft(exported.Payload, exported.Checksum)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range snapshot.Files {
		if strings.Contains(string(file.Data), "synthetic-secret-never-persist") || strings.HasPrefix(file.Name, ".git/") {
			t.Fatal("credential/config leaked")
		}
	}
	retry = true
	result, err := b.PublishSession(recovered, "do not create a new commit", WriteModePRRequired, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if result.CommitSHA != preparedSHA || result.PRNumber != 42 || posts != 1 {
		t.Fatal("PR retry duplicated commit/create", result, posts)
	}
	if err = b.DeleteRevision(recovered, "owner"); err != nil {
		t.Fatal(err)
	}
	_, receipt, err := b.Published(fresh.ID)
	if err != nil || receipt.CommitSHA != preparedSHA {
		t.Fatal("closed receipt lost", err)
	}
	// Remoto avançado após push concluído: retry reconhece ancestralidade.
	direct, err := b.Create("owner", []string{"lab"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = direct.Store.Save(domain.JobDefinition{ID: "direct", Label: "Direct", Team: "lab", JobType: "COMMAND", Params: map[string]interface{}{"command": "echo synthetic"}}); err != nil {
		t.Fatal(err)
	}
	if err = direct.Git.AddAndCommit(".", "prepared direct", "owner", "owner@example.invalid"); err != nil {
		t.Fatal(err)
	}
	sha, err := direct.Git.HeadSHA()
	if err != nil {
		t.Fatal(err)
	}
	direct.State = "publishing"
	direct.publication = `{"mode":"direct","branch":"master","sha":"` + sha + `","actor":"owner"}`
	if err = b.Checkpoint(direct, "owner", "prepare"); err != nil {
		t.Fatal(err)
	}
	if err = direct.Git.PushBranch(testBranch); err != nil {
		t.Fatal(err)
	}
	seed.sync()
	seed.commit("later.txt", "later\n", "later descendant")
	seed.push()
	opened, release, err := b.Open(direct.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	res, err := b.PublishSession(opened, "must not push backwards", WriteModeDirect, "owner")
	if err != nil || res.CommitSHA != sha {
		t.Fatal("descendant lost receipt not recognized", res, err)
	}
}
