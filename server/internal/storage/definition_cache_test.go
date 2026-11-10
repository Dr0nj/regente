package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func BenchmarkDefinitionCopy28800(b *testing.B) {
	defs := make([]domain.JobDefinition, 28800)
	for i := range defs {
		defs[i] = domain.JobDefinition{ID: fmt.Sprintf("job-%05d", i), Team: "graph", JobType: "COMMAND",
			Params:       map[string]any{"command": "echo done", "count": 3},
			ConditionsIn: []string{"parent@stat"}, ConditionsOutAdd: []string{"done@stat"}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if got := cloneDefinitionValues(defs); len(got) != len(defs) || got[0].Params["count"] != 3 {
			b.Fatal("cópia perdeu conteúdo ou tipo")
		}
	}
}

func TestDefinitionCopyNestedMutableValues(t *testing.T) {
	type nested struct {
		Values []int
		Items  [1]map[string]string
		When   time.Time
	}
	original := []domain.JobDefinition{{ID: "original", Params: map[string]any{
		"nested": &nested{Values: []int{3}, Items: [1]map[string]string{{"key": "original"}}, When: time.Now()},
		"nil":    []string(nil), "empty": []string{},
	}, Actions: []domain.ActionRule{{Channels: []string{"mail"}}},
		SubWorkflow: &domain.SubWorkflowRef{Variables: map[string]string{"key": "original"}}}}
	got := cloneDefinitionValues(original)
	if !reflect.DeepEqual(original, got) {
		t.Fatal("cópia alterou tipos, valores, nil ou time.Time")
	}
	got[0].Params["nested"].(*nested).Values[0] = 99
	got[0].Params["nested"].(*nested).Items[0]["key"] = "changed"
	got[0].Actions[0].Channels[0] = "changed"
	got[0].SubWorkflow.Variables["key"] = "changed"
	if n := original[0].Params["nested"].(*nested); n.Values[0] != 3 || n.Items[0]["key"] != "original" ||
		original[0].Actions[0].Channels[0] != "mail" || original[0].SubWorkflow.Variables["key"] != "original" {
		t.Fatal("cópia manteve referências mutáveis ao snapshot")
	}
}

func BenchmarkDefinitionLookup28800(b *testing.B) {
	store := NewFileStore(b.TempDir(), false)
	snap := &definitionSnapshot{definitions: make([]domain.JobDefinition, 28800), byID: map[string]int{}}
	for i := range snap.definitions {
		id := fmt.Sprintf("job-%05d", i)
		snap.definitions[i] = domain.JobDefinition{ID: id, Team: "graph", JobType: "COMMAND",
			Params:       map[string]any{"command": "echo done", "count": 3},
			ConditionsIn: []string{"parent@stat"}, ConditionsOutAdd: []string{"done@stat"}}
		snap.byID[id] = i
	}
	store.cache.current = snap
	store.cache.enabled.Store(true)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		def, found, err := store.DefinitionContext(context.Background(), "job-28799")
		if err != nil || !found || def.ID != "job-28799" {
			b.Fatal("busca falhou", err)
		}
	}
}

func TestDefinitionLookupTracksRevisionAndIsolatesReaders(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprint(cached), func(t *testing.T) {
			store := NewFileStore(t.TempDir(), false)
			def := domain.JobDefinition{ID: "one", Team: "folder", Params: map[string]any{"value": 3}}
			if err := store.Save(def); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cached {
				if err := store.StartDefinitionCache(ctx, time.Hour); err != nil {
					t.Fatal(err)
				}
			}
			got, found, err := store.DefinitionContext(ctx, "one")
			if err != nil || !found || got.Team != "folder" {
				t.Fatalf("busca: %+v %v %v", got, found, err)
			}
			got.Params["value"] = 99
			got, found, err = store.DefinitionContext(ctx, "one")
			if err != nil || !found || got.Params["value"] != 3 {
				t.Fatal("leitor alterou revisão compartilhada", err)
			}
			def.Label = "updated"
			if err = store.Save(def); err != nil {
				t.Fatal(err)
			}
			got, found, err = store.DefinitionContext(ctx, "one")
			if err != nil || !found || got.Label != "updated" {
				t.Fatal("índice manteve revisão antiga", err)
			}
			if err = store.Delete("folder", "one"); err != nil {
				t.Fatal(err)
			}
			if _, found, err = store.DefinitionContext(ctx, "one"); err != nil || found {
				t.Fatal("índice manteve definição removida", err)
			}
		})
	}
}

func TestDefinitionCacheInvalidationAndIsolation(t *testing.T) {
	s := NewFileStore(t.TempDir(), false)
	d := domain.JobDefinition{ID: "a", Team: "f", Params: map[string]any{"count": 3, "nested": map[string]any{"list": []any{"original"}}}, ConditionsOutAdd: []string{"C"}}
	if err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.StartDefinitionCache(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	const n = 10
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defs, err := s.List()
			if err != nil {
				t.Error(err)
				return
			}
			defs[0].ConditionsOutAdd[0] = "changed"
			defs[0].Params["count"] = 9
		}()
	}
	wg.Wait()
	if s.cache.builds.Load() != 1 {
		t.Fatal("construções duplicadas")
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ConditionsOutAdd[0] != "C" || got[0].Params["count"] != 3 {
		t.Fatal("snapshot mutável/tipo alterado")
	}
	d.Label = "new"
	if err = s.Save(d); err != nil {
		t.Fatal(err)
	}
	got, err = s.List()
	if err != nil || got[0].Label != "new" {
		t.Fatalf("save não invalidou: %v", err)
	}
	path := filepath.Join(s.definitionsDir(), "f", "a.yaml")
	if err = os.WriteFile(path, []byte("broken: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.rebuildDefinitionSnapshot(); err == nil {
		t.Fatal("aceitou YAML inválido")
	}
	if _, err = s.List(); err == nil {
		t.Fatal("serviu snapshot velho após erro")
	}
	if err = s.Save(d); err != nil {
		t.Fatal(err)
	}
	if _, err = s.List(); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete("f", "a"); err != nil {
		t.Fatal(err)
	}
	got, err = s.List()
	if err != nil || len(got) != 0 {
		t.Fatal("delete não invalidou")
	}
}

func TestDefinitionCacheExternalContentAndViewScope(t *testing.T) {
	s := NewFileStore(t.TempDir(), false)
	for _, team := range []string{"visible", "hidden"} {
		if err := s.Save(domain.JobDefinition{ID: team, Team: team, Label: "old"}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.StartDefinitionCache(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	filter := func(team, env string) bool { return team == "visible" }
	before, err := s.DefinitionViewDigest(filter)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(s.definitionsDir(), "hidden", "hidden.yaml")
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+3 <= len(raw); i++ {
		if string(raw[i:i+3]) == "old" {
			copy(raw[i:i+3], "new")
		}
	}
	if err = os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(p, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.rebuildDefinitionSnapshot(); err != nil {
		t.Fatal(err)
	}
	if s.cache.builds.Load() != 2 {
		t.Fatal("mtime igual ocultou mudança")
	}
	after, err := s.DefinitionViewDigest(filter)
	if err != nil || after != before {
		t.Fatal("mudança invisível vazou para visão")
	}
	if err = s.Save(domain.JobDefinition{ID: "visible", Team: "visible", Label: "new"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.List(); err != nil {
		t.Fatal(err)
	}
	after, err = s.DefinitionViewDigest(filter)
	if err != nil || after == before {
		t.Fatal("mudança visível não refletida")
	}
}

func TestDefinitionCacheCoordinatedMutation(t *testing.T) {
	s := NewFileStore(t.TempDir(), false)
	if err := s.Save(domain.JobDefinition{ID: "a", Team: "f", Label: "before"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.StartDefinitionCache(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	g := NewGitOps(s.Root(), "", "")
	g.BindDefinitionStore(s)
	finish := g.beginDefinitionMutation()
	done := make(chan string, 1)
	go func() {
		defs, err := s.List()
		if err != nil {
			done <- err.Error()
			return
		}
		done <- defs[0].Label
	}()
	select {
	case <-done:
		finish()
		t.Fatal("leitor atravessou mutação")
	case <-time.After(20 * time.Millisecond):
	}
	err := os.WriteFile(filepath.Join(s.definitionsDir(), "f", "a.yaml"), []byte("id: a\nteam: f\nlabel: after\n"), 0600)
	finish()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case label := <-done:
		if label != "after" {
			t.Fatal(label)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deadlock")
	}
}

func TestDefinitionCacheCancelledReaderAndNonblockingHandshake(t *testing.T) {
	s := NewFileStore(t.TempDir(), false)
	if err := s.Save(domain.JobDefinition{ID: "a", Team: "f"}); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	if err := s.StartDefinitionCache(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	s.cache.build.Lock()
	locked := true
	defer func() {
		if locked {
			s.cache.build.Unlock()
		}
	}()
	if err := s.Save(domain.JobDefinition{ID: "a", Team: "f", Label: "new"}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := s.WorkspaceViewDigest(func(string, string) bool { return true }, func(string) bool { return true }); !errors.Is(err, ErrDefinitionsNotReady) {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("handshake esperou build")
	}
	reader, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ListContext(reader); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		defs, err := s.ListContext(ctx)
		if err == nil && defs[0].Label != "new" {
			err = errors.New("geração antiga")
		}
		done <- err
	}()
	s.cache.build.Unlock()
	locked = false
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("leitor cancelado interrompeu build compartilhado")
	}
	if s.cache.builds.Load() != 2 {
		t.Fatal("stampede", s.cache.builds.Load())
	}
}

func BenchmarkWorkspaceView28800(b *testing.B) {
	s := NewFileStore(b.TempDir(), false)
	dir := filepath.Join(s.definitionsDir(), "f")
	if err := os.MkdirAll(dir, 0700); err != nil {
		b.Fatal(err)
	}
	for i := range 28800 {
		id := fmt.Sprintf("job-%05d", i)
		raw := fmt.Sprintf("id: %s\nteam: f\nconditionsIn: [C%d]\nconditionsOutAdd: [C%d]\n", id, i, i+1)
		if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(raw), 0600); err != nil {
			b.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	if err := s.StartDefinitionCache(ctx, time.Hour); err != nil {
		b.Fatal(err)
	}
	b.Logf("construção fria com leitura/YAML/normalização/hash: %s", time.Since(start))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := s.WorkspaceViewDigest(func(string, string) bool { return true }, func(string) bool { return true }); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGitSyncUnchangedRetainsDefinitionSnapshot(t *testing.T) {
	bare, seed := initRemote(t)
	seed.commit("definitions/f/job.yaml", "id: job\nteam: f\nlabel: original\n", "definition")
	seed.push()
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(ws, false)
	g.BindDefinitionStore(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := store.StartDefinitionCache(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	original, err := store.definitionSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(ws, "scratch.tmp"), "preserved")
	for range 3 {
		if err = g.SyncFromRemote(); err != nil {
			t.Fatal(err)
		}
	}
	same, err := store.readyDefinitionSnapshot()
	if err != nil || same != original || store.cache.builds.Load() != 1 {
		t.Fatal("sync sem mudanças invalidou snapshot", err)
	}
	// Reset continua descartando edição local rastreada, mesmo sem novo commit remoto.
	mustWrite(t, filepath.Join(ws, "definitions", "f", "job.yaml"), "id: job\nteam: f\nlabel: local\n")
	if err = g.SyncFromRemote(); err != nil {
		t.Fatal(err)
	}
	reset, err := store.definitionSnapshot()
	if err != nil || reset.definitions[0].Label != "original" {
		t.Fatal("reset local não aplicado", err)
	}
	seed.commit("definitions/f/job.yaml", "id: job\nteam: f\nlabel: remote\n", "advance")
	seed.push()
	if err = g.SyncFromRemote(); err != nil {
		t.Fatal(err)
	}
	updated, err := store.definitionSnapshot()
	if err != nil || updated.definitions[0].Label != "remote" || updated.generation <= original.generation {
		t.Fatal("snapshot remoto não avançou", err)
	}
	if !exists(filepath.Join(ws, "scratch.tmp")) {
		t.Fatal("sync removeu untracked")
	}
}
