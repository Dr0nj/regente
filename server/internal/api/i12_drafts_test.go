package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/storage"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func i12Source(t *testing.T) (string, *git.Repository, string) {
	t.Helper()
	bare := t.TempDir()
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{bare}}); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"lab", "hidden"} {
		if err = os.MkdirAll(filepath.Join(dir, "definitions", folder), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "definitions", folder, ".keep"), []byte("synthetic\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	i12Commit(t, repo, "initial")
	return bare, repo, dir
}
func i12Commit(t *testing.T, repo *git.Repository, message string) {
	t.Helper()
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err = wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = wt.Commit(message, &git.CommitOptions{Author: &object.Signature{Name: "Lab", Email: "lab@example.invalid", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if err = repo.Push(&git.PushOptions{}); err != nil {
		t.Fatal(err)
	}
}
func i12Request(t *testing.T, srv *httptest.Server, method, path, etag string, body any, want int) ([]byte, string) {
	return i12RequestAs(t, srv, method, path, etag, "test-token", body, want)
}
func i12RequestAs(t *testing.T, srv *httptest.Server, method, path, etag, token string, body any, want int) ([]byte, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, want, data)
	}
	return data, res.Header.Get("ETag")
}
func i12Definition(label string) map[string]any {
	return map[string]any{"id": "draft", "label": label, "team": "lab", "jobType": "COMMAND", "schedule": map[string]any{"enabled": false}, "actionConfig": map[string]any{"command": "echo synthetic-only"}}
}

func TestI12DraftContracts(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			database := machineTestDB(t, dialect)
			t.Run("concurrent-bootstrap", func(t *testing.T) {
				start := make(chan struct{})
				results := make(chan error, 2)
				for i := 0; i < 2; i++ {
					go func() { <-start; results <- auth.Bootstrap(database) }()
				}
				close(start)
				for i := 0; i < 2; i++ {
					if err := <-results; err != nil {
						t.Fatal(err)
					}
				}
				var n int
				if err := database.QueryRow("SELECT COUNT(*) FROM users WHERE username='admin'").Scan(&n); err != nil || n != 1 {
					t.Fatal("bootstrap duplicate/missing", n, err)
				}
			})
			source, seed, seedDir := i12Source(t)
			rootA := t.TempDir()
			a := storage.NewSessionManager(rootA, source, "master", "", nil, database)
			b := storage.NewSessionManager(t.TempDir(), source, "master", "", nil, database)
			if err := b.Restore(); err != nil {
				t.Fatal(err)
			}
			srvA := httptest.NewServer(NewRouter(Config{DB: database, Token: "test-token", Sessions: a, WriteMode: storage.WriteModeDirect}))
			defer srvA.Close()
			srvB := httptest.NewServer(NewRouter(Config{DB: database, Token: "test-token", Sessions: b, WriteMode: storage.WriteModeDirect}))
			defer srvB.Close()
			data, version := i12Request(t, srvA, "POST", "/api/design/sessions", "", map[string]any{"folders": []string{"lab"}}, 201)
			var session storage.DesignSession
			if err := json.Unmarshal(data, &session); err != nil {
				t.Fatal(err)
			}
			endpoint := "/api/design/sessions/" + session.ID
			t.Run("cross-node-restart-and-missing-cache", func(t *testing.T) {
				i12Request(t, srvA, "POST", endpoint+"/definitions", "", i12Definition("unpublished"), 428)
				_, version = i12Request(t, srvA, "POST", endpoint+"/definitions", version, i12Definition("unpublished"), 200)
				dataB, v := i12Request(t, srvB, "GET", endpoint+"/definitions", "", nil, 200)
				if !strings.Contains(string(dataB), "unpublished") || v != version {
					t.Fatal("node B lost content/revision")
				}
				if err := os.RemoveAll(rootA); err != nil {
					t.Fatal(err)
				}
				c := storage.NewSessionManager(t.TempDir(), source, "master", "", nil, database)
				if err := c.Restore(); err != nil {
					t.Fatal(err)
				}
				opened, cleanup, err := c.Open(session.ID)
				if err != nil {
					t.Fatal(err)
				}
				defer cleanup()
				defs, err := opened.Store.List()
				if err != nil || len(defs) != 1 || defs[0].Label != "unpublished" || !opened.Dirty() {
					t.Fatal("restart lost dirty snapshot")
				}
			})
			t.Run("concurrent-CAS-and-all-stale-mutations", func(t *testing.T) {
				var wg sync.WaitGroup
				codes := make(chan int, 2)
				for _, srv := range []*httptest.Server{srvA, srvB} {
					wg.Add(1)
					go func(srv *httptest.Server) {
						defer wg.Done()
						raw, _ := json.Marshal(i12Definition("winner"))
						req, _ := http.NewRequest("POST", srv.URL+endpoint+"/definitions", strings.NewReader(string(raw)))
						req.Header.Set("Authorization", "Bearer test-token")
						req.Header.Set("If-Match", version)
						res, err := srv.Client().Do(req)
						if err != nil {
							codes <- 0
							return
						}
						defer res.Body.Close()
						_, _ = io.Copy(io.Discard, res.Body)
						codes <- res.StatusCode
					}(srv)
				}
				wg.Wait()
				close(codes)
				counts := map[int]int{}
				for code := range codes {
					counts[code]++
				}
				if counts[200] != 1 || counts[409] != 1 {
					t.Fatalf("CAS results %v", counts)
				}
				stale := version
				_, version = i12Request(t, srvB, "GET", endpoint+"/definitions", "", nil, 200)
				for _, route := range []struct{ method, suffix string }{{"POST", "/definitions"}, {"DELETE", "/definitions/lab/draft"}, {"POST", "/folders"}, {"POST", "/folders/open"}, {"PUT", "/folders/lab/layout"}, {"POST", "/code"}, {"POST", "/bulk"}, {"POST", "/massupdate"}, {"POST", "/massupdate/undo"}, {"POST", "/publish"}, {"DELETE", ""}} {
					i12Request(t, srvA, route.method, endpoint+route.suffix, stale, map[string]any{}, 409)
				}
			})
			t.Run("CODE-bulk-mass-undo-and-layout", func(t *testing.T) {
				code := "id: draft\nlabel: Code edit\nteam: lab\njobType: COMMAND\nschedule: {enabled: false}\nparams: {command: echo synthetic-only}\n"
				_, version = i12Request(t, srvB, "POST", endpoint+"/code", version, map[string]any{"code": code, "apply": true}, 200)
				_, version = i12Request(t, srvA, "POST", endpoint+"/bulk", version, map[string]any{"action": "patch", "ids": []string{"draft"}, "patch": map[string]any{"retries": 2}}, 200)
				_, version = i12Request(t, srvA, "POST", endpoint+"/massupdate", version, map[string]any{"criteria": map[string]any{"ids": []string{"draft"}}, "operation": map[string]any{"op": "set-field", "field": "label", "value": "Mass edit"}, "apply": true}, 200)
				_, version = i12Request(t, srvB, "POST", endpoint+"/massupdate/undo", version, map[string]any{}, 200)
				data, _ := i12Request(t, srvB, "GET", endpoint+"/definitions", "", nil, 200)
				if !strings.Contains(string(data), "Code edit") || !strings.Contains(string(data), "\"retries\":2") {
					t.Fatalf("undo across nodes: %s", data)
				}
				_, version = i12Request(t, srvB, "PUT", endpoint+"/folders/lab/layout", version, map[string]any{"columns": 6}, 200)
				data, _ = i12Request(t, srvA, "GET", endpoint+"/folders", "", nil, 200)
				if !strings.Contains(string(data), "\"columns\":6") {
					t.Fatalf("layout lost: %s", data)
				}
			})
			t.Run("verified-export-import-no-overwrite", func(t *testing.T) {
				data, _ := i12Request(t, srvB, "GET", endpoint+"/export", "", nil, 200)
				var export storage.DraftExport
				if json.Unmarshal(data, &export) != nil {
					t.Fatal("export")
				}
				imported, err := b.Import(export, "system")
				if err != nil {
					t.Fatal(err)
				}
				if imported.ID == session.ID {
					t.Fatal("import overwrote original")
				}
				opened, cleanup, err := a.Open(imported.ID)
				if err != nil {
					t.Fatal(err)
				}
				defer cleanup()
				defs, err := opened.Store.List()
				if err != nil || len(defs) != 1 || defs[0].Label != "Code edit" {
					t.Fatal("import lost content")
				}
				export.Checksum = "tampered"
				if _, err = b.Import(export, "system"); err == nil {
					t.Fatal("tampered export accepted")
				}
			})
			t.Run("Git-conflict-preserves-draft", func(t *testing.T) {
				if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("remote advance"), 0644); err != nil {
					t.Fatal(err)
				}
				i12Commit(t, seed, "remote advance")
				i12Request(t, srvB, "POST", endpoint+"/publish", version, map[string]any{}, 409)
				data, _ := i12Request(t, srvA, "GET", endpoint+"/definitions", "", nil, 200)
				if !strings.Contains(string(data), "Code edit") {
					t.Fatal("conflict lost draft")
				}
				meta, err := a.Metadata(session.ID)
				if err != nil || meta.State != "active" || !meta.Dirty() {
					t.Fatal("conflict changed draft lifecycle")
				}
				// Resolução revisada: nova base preserva o commit remoto e o draft antigo.
				reviewed, err := a.Create("system", []string{"lab"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				reviewEP := "/api/design/sessions/" + reviewed.ID
				codeData, _ := i12Request(t, srvA, "GET", endpoint+"/code", "", nil, 200)
				var code struct{ Code string }
				if json.Unmarshal(codeData, &code) != nil {
					t.Fatal("review code")
				}
				_, rv := i12Request(t, srvB, "POST", reviewEP+"/code", draftETag(reviewed), map[string]any{"code": code.Code, "apply": true}, 200)
				i12Request(t, srvB, "POST", reviewEP+"/publish", rv, map[string]any{}, 200)
				bare, err := git.PlainOpen(source)
				if err != nil {
					t.Fatal(err)
				}
				head, err := bare.Head()
				if err != nil {
					t.Fatal(err)
				}
				commit, err := bare.CommitObject(head.Hash())
				if err != nil {
					t.Fatal(err)
				}
				tree, err := commit.Tree()
				if err != nil {
					t.Fatal(err)
				}
				file, err := tree.File("README.md")
				if err != nil {
					t.Fatal(err)
				}
				contents, err := file.Contents()
				if err != nil || contents != "remote advance" {
					t.Fatal("review overwrote remote changes", contents, err)
				}
				if _, err = a.Metadata(session.ID); err != nil {
					t.Fatal("review removed original draft")
				}

			})
			t.Run("legacy-preserved-and-migrated", func(t *testing.T) {
				legacyRoot := t.TempDir()
				legacyManager := storage.NewSessionManager(legacyRoot, source, "master", "", nil, nil)
				legacy, err := legacyManager.Create("legacy", []string{"lab"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err = legacy.Store.Save(storageTestDefinition()); err != nil {
					t.Fatal(err)
				}
				expectedLegacy, err := legacy.Store.List()
				if err != nil {
					t.Fatal(err)
				}
				expectedJSON, _ := json.Marshal(expectedLegacy)
				for _, entry := range []struct{ id, path string }{{legacy.ID, legacy.Path}, {"missing-legacy", filepath.Join(legacyRoot, "missing")}} {
					if _, err = database.Exec("INSERT INTO design_sessions(id,actor,folders_json,new_folders_json,base_sha,path,created_at,last_touch) VALUES(?,'legacy','[\"lab\"]','[]',?,?,?,?)", entry.id, legacy.BaseSHA, entry.path, time.Now(), time.Now()); err != nil {
						t.Fatal(err)
					}
				}
				if err = b.Restore(); err != nil {
					t.Fatal(err)
				}
				meta, err := b.Metadata(legacy.ID)
				if err != nil || !meta.RecoveryRequired {
					t.Fatal("foreign clone adopted/deleted")
				}
				migration := storage.NewSessionManager(legacyRoot, source, "master", "", nil, database)
				if err = migration.Restore(); err != nil {
					t.Fatal(err)
				}
				opened, cleanup, err := b.Open(legacy.ID)
				if err != nil {
					t.Fatal(err)
				}
				defer cleanup()
				defs, err := opened.Store.List()
				actualJSON, _ := json.Marshal(defs)
				if err != nil || string(actualJSON) != string(expectedJSON) {
					t.Fatalf("legacy content lost: %v %s", err, actualJSON)
				}
				if _, err = os.Stat(legacy.Path); err != nil {
					t.Fatal("migration removed original")
				}
				missing, err := b.Metadata("missing-legacy")
				if err != nil || !missing.RecoveryRequired {
					t.Fatal("missing legacy row lost")
				}
			})

			t.Run("ownership-ACL-and-admin-audit", func(t *testing.T) {
				owner, err := auth.CreateUser(database, "draft-owner", "strong-test-password", auth.RoleOperator)
				if err != nil {
					t.Fatal(err)
				}
				_, err = auth.CreateUser(database, "other-operator", "strong-test-password", auth.RoleOperator)
				if err != nil {
					t.Fatal(err)
				}
				ownerToken, _, err := auth.Login(database, "draft-owner", "strong-test-password")
				if err != nil {
					t.Fatal(err)
				}
				otherToken, _, err := auth.Login(database, "other-operator", "strong-test-password")
				if err != nil {
					t.Fatal(err)
				}
				if err = auth.SetUserACL(database, owner.ID, "lab", "rw"); err != nil {
					t.Fatal(err)
				}
				owned, err := a.Create(owner.Username, []string{"lab"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				ep := "/api/design/sessions/" + owned.ID
				v := draftETag(owned)
				for _, suffix := range []string{"", "/definitions", "/code", "/export"} {
					i12RequestAs(t, srvB, "GET", ep+suffix, "", otherToken, nil, 403)
				}
				i12RequestAs(t, srvB, "DELETE", ep, v, otherToken, nil, 403)
				i12RequestAs(t, srvA, "GET", ep+"/export", "", ownerToken, nil, 403)
				data, _ := i12RequestAs(t, srvB, "GET", ep+"/folders", "", ownerToken, nil, 200)
				if strings.Contains(string(data), "hidden") {
					t.Fatal("folder ACL leaked")
				}
				_, v = i12RequestAs(t, srvA, "POST", ep+"/definitions", v, ownerToken, i12Definition("owned edit"), 200)
				// Destino alterado fora das folders declaradas também precisa ACL no publish.
				if err = auth.SetUserACL(database, owner.ID, "hidden", "rw"); err != nil {
					t.Fatal(err)
				}
				hidden := i12Definition("Hidden edit")
				hidden["id"] = "hidden-edited"
				hidden["team"] = "hidden"
				_, v = i12RequestAs(t, srvA, "POST", ep+"/definitions", v, ownerToken, hidden, 200)
				if err = auth.SetUserACL(database, owner.ID, "hidden", "r"); err != nil {
					t.Fatal(err)
				}
				i12RequestAs(t, srvB, "POST", ep+"/publish", v, ownerToken, map[string]any{}, 403)
				// Write revogado mas read mantido: leitura funciona, publicação e edição não.
				if err = auth.SetUserACL(database, owner.ID, "lab", "r"); err != nil {
					t.Fatal(err)
				}
				i12RequestAs(t, srvB, "GET", ep+"/definitions", "", ownerToken, nil, 200)
				i12RequestAs(t, srvB, "POST", ep+"/definitions", v, ownerToken, i12Definition("revoked"), 403)
				i12RequestAs(t, srvB, "POST", ep+"/publish", v, ownerToken, map[string]any{}, 403)
				_, v = i12Request(t, srvB, "POST", ep+"/definitions", v, i12Definition("admin reviewed"), 200)
				var actor string
				if err = database.QueryRow("SELECT actor FROM design_draft_audit WHERE session_id=? ORDER BY revision DESC LIMIT 1", owned.ID).Scan(&actor); err != nil || actor != "system" {
					t.Fatal("admin override unaudited", actor, err)
				}
				if err = auth.SetUserACL(database, owner.ID, "hidden", "r"); err != nil {
					t.Fatal(err)
				}
				if err = auth.SetUserACL(database, owner.ID, "lab", ""); err != nil {
					t.Fatal(err)
				}
				i12RequestAs(t, srvB, "GET", ep+"/code", "", ownerToken, nil, 403)
			})
			t.Run("publish-and-lost-receipt-recovery", func(t *testing.T) {
				fresh, err := a.Create("system", []string{"lab"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				ep := "/api/design/sessions/" + fresh.ID
				_, v := i12Request(t, srvA, "POST", ep+"/definitions", draftETag(fresh), i12Definition("Published normally"), 200)
				published, _ := i12Request(t, srvB, "POST", ep+"/publish", v, map[string]any{}, 200)
				if !strings.Contains(string(published), "commitSha") {
					t.Fatalf("publish: %s", published)
				}
				i12Request(t, srvA, "GET", ep, "", nil, 404)
				replay, _ := i12Request(t, srvA, "POST", ep+"/publish", v, map[string]any{}, 200)
				if string(replay) != string(published) {
					t.Fatal("closed publication receipt differs")
				}
				lost, err := a.Create("system", []string{"lab"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				opened, cleanup, err := a.Open(lost.ID)
				if err != nil {
					t.Fatal(err)
				}
				defer cleanup()
				def := storageTestDefinition()
				def.ID = "receipt"
				def.Label = "Receipt recovery"
				if err = opened.Store.Save(def); err != nil {
					t.Fatal(err)
				}
				if err = a.Checkpoint(opened, "system", "fixture-save"); err != nil {
					t.Fatal(err)
				}
				if err = opened.Git.AddAndCommit(".", "synthetic prepared publication", "system", "system@example.invalid"); err != nil {
					t.Fatal(err)
				}
				sha, err := opened.Git.HeadSHA()
				if err != nil {
					t.Fatal(err)
				}
				if err = a.Checkpoint(opened, "system", "fixture-prepare"); err != nil {
					t.Fatal(err)
				}
				intent, _ := json.Marshal(map[string]any{"mode": "direct", "branch": "master", "sha": sha, "actor": "system"})
				if _, err = database.Exec("UPDATE design_sessions SET state='publishing',publication=? WHERE id=?", string(intent), opened.ID); err != nil {
					t.Fatal(err)
				}
				if err = opened.Git.PushBranch("master"); err != nil {
					t.Fatal(err)
				} // queda antes do receipt no banco
				if err = os.RemoveAll(opened.Path); err != nil {
					t.Fatal(err)
				}
				recovered, done, err := b.Open(lost.ID)
				if err != nil {
					t.Fatal(err)
				}
				defer done()
				res, err := b.PublishSession(recovered, "must not create another commit", storage.WriteModeDirect, "system")
				if err != nil {
					t.Fatal(err)
				}
				if res.CommitSHA != sha || res.Mode != "direct" {
					t.Fatal("lost receipt created duplicate commit", res)
				}
				if err = b.DeleteRevision(recovered, "system"); err != nil {
					t.Fatal(err)
				}
			})
			t.Run("database-failure-never-acknowledges-local-save", func(t *testing.T) {
				fresh, err := a.Create("system", []string{"lab"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				ep := "/api/design/sessions/" + fresh.ID
				if dialect == db.SQLite {
					if _, err = database.Exec("CREATE TRIGGER i12_reject_snapshot BEFORE INSERT ON design_draft_versions BEGIN SELECT RAISE(ABORT, 'synthetic unavailable'); END"); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err = database.Exec("CREATE FUNCTION i12_reject_snapshot() RETURNS trigger LANGUAGE plpgsql AS $ BEGIN RAISE EXCEPTION 'synthetic unavailable'; END $"); err != nil {
						t.Fatal(err)
					}
					if _, err = database.Exec("CREATE TRIGGER i12_reject_snapshot BEFORE INSERT ON design_draft_versions FOR EACH ROW EXECUTE FUNCTION i12_reject_snapshot()"); err != nil {
						t.Fatal(err)
					}
				}
				i12Request(t, srvA, "POST", ep+"/definitions", draftETag(fresh), i12Definition("must rollback"), 503)
				data, _ := i12Request(t, srvB, "GET", ep+"/definitions", "", nil, 200)
				if strings.Contains(string(data), "must rollback") {
					t.Fatal("failed DB save leaked cache content")
				}
				current, err := b.Metadata(fresh.ID)
				if err != nil || current.Revision != 1 {
					t.Fatal("failed transaction advanced revision")
				}
				if dialect == db.SQLite {
					_, err = database.Exec("DROP TRIGGER i12_reject_snapshot")
				} else {
					_, err = database.Exec("DROP TRIGGER i12_reject_snapshot ON design_draft_versions")
				}
				if err != nil {
					t.Fatal(err)
				}
			})
			t.Run("unsafe-path-and-discard-CAS", func(t *testing.T) {
				bad := i12Definition("bad")
				bad["id"] = "../../escaped"
				i12Request(t, srvB, "POST", endpoint+"/definitions", version, bad, 500)
				i12Request(t, srvB, "DELETE", endpoint, version, nil, 204)
				i12Request(t, srvA, "GET", endpoint, "", nil, 404)
				var audit int
				if err := database.QueryRow("SELECT COUNT(*) FROM design_draft_audit WHERE session_id=?", session.ID).Scan(&audit); err != nil || audit < 6 {
					t.Fatalf("audit missing: %d %v", audit, err)
				}
			})
		})
	}
}
func storageTestDefinition() domain.JobDefinition {
	return domain.JobDefinition{ID: "legacy", Label: "Legacy edit", Team: "lab", JobType: "COMMAND", Params: map[string]interface{}{"command": "echo synthetic"}}
}
