package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/telemetry"
	"gopkg.in/yaml.v3"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

type definitionSnapshot struct {
	definitions []domain.JobDefinition
	digest      string
	views       []definitionView
	folders     []FolderInfo
	generation  uint64
}
type definitionView struct {
	team, environment string
	hash              [32]byte
}
type definitionCache struct {
	enabled                atomic.Bool
	mu                     sync.Mutex
	build                  sync.Mutex
	current                *definitionSnapshot
	err                    error
	ctx                    context.Context
	trigger                chan struct{}
	flight                 chan struct{}
	generation             uint64
	hits, builds, failures atomic.Uint64
}

// StartDefinitionCache ativa somente no workspace vivo. Sessions mantêm leitura
// direta. Edições externas são reconciliadas pelo conteúdo em intervalo explícito.
// Chamadas quentes não leem arquivos nem reconstroem o índice. Shutdown cancela
// a reconciliação; o snapshot é propriedade deste FileStore, sem registro global.
func (s *FileStore) StartDefinitionCache(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("definition reconciliation interval must be positive")
	}
	s.cache.mu.Lock()
	if s.cache.trigger == nil {
		s.cache.ctx = ctx
		s.cache.trigger = make(chan struct{}, 1)
	}
	s.cache.mu.Unlock()
	if !s.cache.enabled.CompareAndSwap(false, true) {
		return nil
	}
	if _, err := s.rebuildDefinitionSnapshot(); err != nil {
		s.cache.enabled.Store(false)
		return err
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_, _ = s.rebuildDefinitionSnapshot()
			case <-s.cache.trigger:
				_, _ = s.definitionSnapshotContext(ctx)
			}
		}
	}()
	return nil
}

// beginWorkspaceMutation também envolve operações Git que alteram o worktree.
// Invalida antes e depois, inclusive em falha parcial. Nunca publica geração
// antiga: build e publicação mantêm RLock contra todas as escritas coordenadas.
func (s *FileStore) beginWorkspaceMutation() func() {
	s.mu.Lock()
	s.invalidateDefinitionsLocked()
	return func() {
		s.invalidateDefinitionsLocked()
		s.mu.Unlock()
		s.cache.mu.Lock()
		trigger := s.cache.trigger
		s.cache.mu.Unlock()
		if trigger != nil {
			select {
			case trigger <- struct{}{}:
			default:
			}
		}
	}
}
func (s *FileStore) invalidateDefinitionsLocked() {
	s.cache.mu.Lock()
	s.cache.current = nil
	s.cache.err = nil
	s.cache.generation++
	s.cache.mu.Unlock()
}

var ErrDefinitionsNotReady = errors.New("workspace definitions are rebuilding or unavailable")

func (s *FileStore) definitionSnapshot() (*definitionSnapshot, error) {
	return s.definitionSnapshotContext(context.Background())
}
func (s *FileStore) definitionSnapshotContext(ctx context.Context) (*definitionSnapshot, error) {
	s.cache.mu.Lock()
	snap, err := s.cache.current, s.cache.err
	if snap != nil {
		s.cache.mu.Unlock()
		s.cache.hits.Add(1)
		telemetry.Count(telemetry.CacheHit, 1)
		return snap, nil
	}
	if err != nil {
		s.cache.mu.Unlock()
		return nil, err
	}
	flight := s.cache.flight
	if flight == nil {
		flight = make(chan struct{})
		s.cache.flight = flight
		go func() {
			_, _ = s.rebuildDefinitionSnapshot()
			s.cache.mu.Lock()
			close(flight)
			s.cache.flight = nil
			s.cache.mu.Unlock()
		}()
	}
	s.cache.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight:
	}
	s.cache.mu.Lock()
	snap, err = s.cache.current, s.cache.err
	s.cache.mu.Unlock()
	if snap == nil && err == nil {
		err = ErrDefinitionsNotReady
	}
	return snap, err
}
func (s *FileStore) readyDefinitionSnapshot() (*definitionSnapshot, error) {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	if s.cache.current == nil {
		return nil, ErrDefinitionsNotReady
	}
	s.cache.hits.Add(1)
	telemetry.Count(telemetry.CacheHit, 1)
	return s.cache.current, nil
}
func (s *FileStore) DefinitionsReady() bool {
	if !s.cache.enabled.Load() {
		return true
	}
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	return s.cache.current != nil
}
func (s *FileStore) ListContext(ctx context.Context) ([]domain.JobDefinition, error) {
	if !s.cache.enabled.Load() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return s.List()
	}
	snap, err := s.definitionSnapshotContext(ctx)
	if err != nil {
		return nil, err
	}
	return cloneDefinitionValues(snap.definitions), nil
}

func (s *FileStore) definitionsDigestContext(ctx context.Context) (string, error) {
	defer telemetry.Measure(telemetry.DefinitionsDigest)()
	h := sha256.New()
	err := filepath.WalkDir(s.definitionsDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == s.definitionsDir() {
				return nil
			}
			return err
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(s.definitionsDir(), path)
			fmt.Fprintf(h, "dir:%s\n", rel)
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".yaml") && !strings.HasSuffix(d.Name(), ".yml") {
			return nil
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(s.definitionsDir(), path)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		fmt.Fprintf(h, "%d:%s:%x\n", len(rel), rel, sum)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}
func (s *FileStore) rebuildDefinitionSnapshot() (*definitionSnapshot, error) {
	defer telemetry.Measure(telemetry.DefinitionsBuild)()
	s.cache.build.Lock()
	defer s.cache.build.Unlock()
	s.cache.mu.Lock()
	parent := s.cache.ctx
	s.cache.mu.Unlock()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	s.mu.RLock()
	defer s.mu.RUnlock()
	fail := func(err error) (*definitionSnapshot, error) {
		s.cache.mu.Lock()
		s.cache.current = nil
		s.cache.err = err
		s.cache.mu.Unlock()
		s.cache.failures.Add(1)
		telemetry.Count(telemetry.CacheFailure, 1)
		return nil, err
	}
	before, err := s.definitionsDigestContext(ctx)
	if err != nil {
		return fail(err)
	}
	s.cache.mu.Lock()
	current := s.cache.current
	s.cache.mu.Unlock()
	if current != nil && current.digest == before {
		return current, nil
	}
	defs, folders, observed, err := s.captureDefinitionSnapshot(ctx)
	if err != nil {
		return fail(err)
	}
	after, err := s.definitionsDigestContext(ctx)
	if err != nil {
		return fail(err)
	}
	if after != before || observed != before {
		return fail(fmt.Errorf("definitions changed while building snapshot; retry after reconciliation"))
	}
	snap := &definitionSnapshot{definitions: defs, digest: after, folders: folders}
	for _, def := range defs {
		raw, e := json.Marshal(def)
		if e != nil {
			return fail(e)
		}
		snap.views = append(snap.views, definitionView{def.Team, def.Environment, sha256.Sum256(raw)})
	}
	s.cache.mu.Lock()
	s.cache.generation++
	snap.generation = s.cache.generation
	s.cache.current = snap
	s.cache.err = nil
	s.cache.mu.Unlock()
	s.cache.builds.Add(1)
	telemetry.Count(telemetry.CacheBuild, 1)
	return snap, nil
}

// DefinitionViewDigest não entrega maps/slices privados ao filtro e não copia
// nem serializa todo o grafo por conexão. Apenas hashes visíveis participam.
func (s *FileStore) DefinitionViewDigest(visible func(team, environment string) bool) (string, error) {
	if !s.cache.enabled.Load() {
		defs, err := s.List()
		if err != nil {
			return "", err
		}
		h := sha256.New()
		for _, d := range defs {
			if visible(d.Team, d.Environment) {
				raw, e := json.Marshal(d)
				if e != nil {
					return "", e
				}
				sum := sha256.Sum256(raw)
				h.Write(sum[:])
			}
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	snap, err := s.readyDefinitionSnapshot()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, v := range snap.views {
		if visible(v.team, v.environment) {
			h.Write(v.hash[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Preserva tipos de Params provenientes do YAML (JSON round-trip troca inteiros
// por float64). Structs imutáveis, como time.Time, mantêm campos privados por valor.
func cloneDefinitionValues(in []domain.JobDefinition) []domain.JobDefinition {
	return cloneDefinitionValue(reflect.ValueOf(in)).Interface().([]domain.JobDefinition)
}
func cloneDefinitionValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneDefinitionValue(v.Elem()))
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(cloneDefinitionValue(v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneDefinitionValue(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		it := v.MapRange()
		for it.Next() {
			out.SetMapIndex(it.Key(), cloneDefinitionValue(it.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				out.Field(i).Set(cloneDefinitionValue(v.Field(i)))
			}
		}
		return out
	default:
		return v
	}
}

// Uma única revisão fornece definições e folders; WS nunca reconstrói no handshake.
func (s *FileStore) WorkspaceViewDigest(visible func(string, string) bool, folderVisible func(string) bool) (string, error) {
	defer telemetry.Measure(telemetry.WorkspaceView)()
	var snap *definitionSnapshot
	if s.cache.enabled.Load() {
		var err error
		snap, err = s.readyDefinitionSnapshot()
		if err != nil {
			return "", err
		}
	} else {
		defs, err := s.List()
		if err != nil {
			return "", err
		}
		folders, err := s.ListFolders()
		if err != nil {
			return "", err
		}
		snap = &definitionSnapshot{folders: folders}
		for _, d := range defs {
			raw, err := json.Marshal(d)
			if err != nil {
				return "", err
			}
			snap.views = append(snap.views, definitionView{d.Team, d.Environment, sha256.Sum256(raw)})
		}
	}
	h := sha256.New()
	for _, v := range snap.views {
		if visible(v.team, v.environment) {
			h.Write(v.hash[:])
		}
	}
	for _, f := range snap.folders {
		if folderVisible(f.Name) {
			raw, _ := json.Marshal(f)
			sum := sha256.Sum256(raw)
			h.Write(sum[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// O digest publicado cobre os MESMOS bytes decodificados. Uma alteração ABA
// entre a verificação inicial/final não consegue esconder conteúdo intermediário
// diferente lido pelo parser. Escritas externas multifile devem usar o coordenador
// (ou publicar um diretório/revisão atomicamente), não uma sequência parcial.
func (s *FileStore) captureDefinitionSnapshot(ctx context.Context) ([]domain.JobDefinition, []FolderInfo, string, error) {
	defer telemetry.Measure(telemetry.DefinitionsRead)()
	h := sha256.New()
	defs := []domain.JobDefinition{}
	folders := map[string]*FolderInfo{}
	names := []string{}
	err := filepath.WalkDir(s.definitionsDir(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == s.definitionsDir() {
				return nil
			}
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(s.definitionsDir(), path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if entry.IsDir() {
			fmt.Fprintf(h, "dir:%s\n", rel)
			if rel != "." && len(parts) == 1 {
				name := strings.TrimPrefix(rel, ".archived-")
				folders[rel] = &FolderInfo{Name: name, Archived: strings.HasPrefix(rel, ".archived-")}
				names = append(names, rel)
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		fmt.Fprintf(h, "%d:%s:%x\n", len(rel), rel, sum)
		if entry.Name() == ".regente-folder.yaml" {
			if len(parts) == 2 {
				var meta folderMeta
				_ = yaml.Unmarshal(raw, &meta)
				folders[parts[0]].Layout = meta.Layout
			}
			return nil
		}
		if len(parts) == 2 {
			folders[parts[0]].JobCount++
		}
		var def domain.JobDefinition
		if err = yaml.Unmarshal(raw, &def); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if def.Team == "" && len(parts) > 1 {
			def.Team = parts[0]
		}
		defs = append(defs, def)
		return nil
	})
	if err != nil {
		return nil, nil, "", err
	}
	out := make([]FolderInfo, 0, len(names))
	for _, name := range names {
		out = append(out, *folders[name])
	}
	end := telemetry.Measure(telemetry.DefinitionsNormalize)
	defs = domain.NormalizeConditions(defs)
	end()
	return defs, out, hex.EncodeToString(h.Sum(nil)), nil
}
