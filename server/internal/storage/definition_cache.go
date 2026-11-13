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
	byID        map[string]int
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
	buildTimeout           time.Duration
	trigger                chan struct{}
	flight                 chan struct{}
	generation             uint64
	hits, builds, failures atomic.Uint64
	// Carimbo de stat da última reconciliação bem-sucedida (reconcileDefinitions).
	statStamp  string
	statRounds uint64
}

// StartDefinitionCache ativa somente no workspace vivo. Sessions mantêm leitura
// direta. Edições externas são reconciliadas pelo conteúdo em intervalo explícito.
// Chamadas quentes não leem arquivos nem reconstroem o índice. Shutdown cancela
// a reconciliação; o snapshot é propriedade deste FileStore, sem registro global.
func (s *FileStore) StartDefinitionCache(ctx context.Context, interval time.Duration) error {
	return s.StartDefinitionCacheWithTimeout(ctx, interval, 30*time.Second)
}

// O orçamento de construção é independente da frequência de reconciliação.
// Workspaces grandes em CPUs limitadas precisam de mais tempo inclusive no boot.
func (s *FileStore) StartDefinitionCacheWithTimeout(ctx context.Context, interval, buildTimeout time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("definition reconciliation interval must be positive")
	}
	if buildTimeout <= 0 {
		return fmt.Errorf("definition build timeout must be positive")
	}
	s.cache.mu.Lock()
	if !s.cache.enabled.Load() {
		s.cache.ctx = ctx
		s.cache.buildTimeout = buildTimeout
	}
	if s.cache.trigger == nil {
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
		t := time.NewTimer(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.reconcileDefinitions(ctx)
				t.Reset(interval)
			case <-s.cache.trigger:
				_, _ = s.definitionSnapshotContext(ctx)
			}
		}
	}()
	return nil
}

// definitionContentAuditEvery — a cada quantas reconciliações periódicas o
// conteúdo é relido mesmo com o carimbo de stat inalterado.
const definitionContentAuditEvery = 10

// reconcileDefinitions é a reconciliação PERIÓDICA contra edições feitas por
// fora do server. Antes, cada rodada lia e fazia SHA-256 de todos os YAMLs
// (17% da CPU do server ocioso com 28,8 mil definitions, medido em produção).
// Agora compara primeiro um carimbo barato — caminho, tamanho e mtime de cada
// arquivo — e só relê o conteúdo se o carimbo mudou. Uma edição que preserve
// tamanho E mtime escaparia do carimbo; por isso o conteúdo ainda é conferido
// a cada definitionContentAuditEvery rodadas.
func (s *FileStore) reconcileDefinitions(ctx context.Context) {
	stamp, err := s.definitionsStatStamp(ctx)
	s.cache.mu.Lock()
	s.cache.statRounds++
	same := err == nil && s.cache.current != nil && stamp == s.cache.statStamp && s.cache.statRounds%definitionContentAuditEvery != 0
	s.cache.mu.Unlock()
	if same {
		return
	}
	if _, buildErr := s.rebuildDefinitionSnapshot(); buildErr == nil && err == nil {
		s.cache.mu.Lock()
		s.cache.statStamp = stamp
		s.cache.mu.Unlock()
	}
}

// definitionsStatStamp resume o diretório de definitions sem ler nenhum arquivo.
func (s *FileStore) definitionsStatStamp(ctx context.Context) (string, error) {
	h := sha256.New()
	root := s.definitionsDir()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == root {
				return nil
			}
			return err
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			fmt.Fprintf(h, "dir:%s\n", rel)
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".yaml") && !strings.HasSuffix(d.Name(), ".yml") {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		fmt.Fprintf(h, "%d:%s:%d:%d\n", len(rel), rel, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
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

// DefinitionContext devolve uma cópia isolada de uma definição. A revisão
// cacheada tem índice próprio; autorizar uma ordem não copia o workspace inteiro.
func (s *FileStore) DefinitionContext(ctx context.Context, id string) (domain.JobDefinition, bool, error) {
	if !s.cache.enabled.Load() {
		defs, err := s.ListContext(ctx)
		if err != nil {
			return domain.JobDefinition{}, false, err
		}
		for _, def := range defs {
			if def.ID == id {
				return def, true, nil
			}
		}
		return domain.JobDefinition{}, false, nil
	}
	snap, err := s.definitionSnapshotContext(ctx)
	if err != nil {
		return domain.JobDefinition{}, false, err
	}
	index, found := snap.byID[id]
	if !found {
		return domain.JobDefinition{}, false, nil
	}
	return cloneDefinitionValue(reflect.ValueOf(snap.definitions[index])).Interface().(domain.JobDefinition), true, nil
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
	buildTimeout := s.cache.buildTimeout
	s.cache.mu.Unlock()
	if parent == nil {
		parent = context.Background()
	}
	if buildTimeout <= 0 {
		buildTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, buildTimeout)
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
	snap := &definitionSnapshot{definitions: defs, byID: make(map[string]int, len(defs)), digest: after, folders: folders}
	for index, def := range defs {
		// Mantém a primeira ocorrência, como a busca linear anterior.
		if _, found := snap.byID[def.ID]; !found {
			snap.byID[def.ID] = index
		}
		raw, e := json.Marshal(def)
		if e != nil {
			return fail(e)
		}
		snap.views = append(snap.views, definitionView{def.Team, def.Environment, sha256.Sum256(raw)})
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
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

// O layout mutável depende do tipo, não do job/revisão. Calcula uma vez para
// evitar inspecionar todos os campos escalares de cada definição a cada leitura.
var definitionCloneFields sync.Map // reflect.Type -> []int; somente layouts, sem valores do workspace

func mutableDefinitionFields(t reflect.Type) []int {
	if cached, ok := definitionCloneFields.Load(t); ok {
		return cached.([]int)
	}
	fields := []int{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.IsExported() && definitionTypeNeedsClone(field.Type) {
			fields = append(fields, i)
		}
	}
	cached, _ := definitionCloneFields.LoadOrStore(t, fields)
	return cached.([]int)
}

func definitionTypeNeedsClone(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Slice, reflect.Map:
		return true
	case reflect.Array:
		return definitionTypeNeedsClone(t.Elem())
	case reflect.Struct:
		return len(mutableDefinitionFields(t)) > 0
	default:
		return false
	}
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
		if definitionTypeNeedsClone(v.Type().Elem()) {
			for i := 0; i < v.Len(); i++ {
				out.Index(i).Set(cloneDefinitionValue(v.Index(i)))
			}
		} else {
			reflect.Copy(out, v)
		}
		return out
	case reflect.Array:
		if !definitionTypeNeedsClone(v.Type().Elem()) {
			return v
		}
		out := reflect.New(v.Type()).Elem()
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
		fields := mutableDefinitionFields(v.Type())
		if len(fields) == 0 {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for _, i := range fields {
			out.Field(i).Set(cloneDefinitionValue(v.Field(i)))
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
