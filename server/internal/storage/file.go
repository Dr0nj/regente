// Package storage — persistência de definitions em disco (YAML) com
// opção de commit via git.
//
// Layout:
//
//	<workspace>/definitions/<team>/<id>.yaml
//
// Cada save/delete pode gerar um commit se --git-commit=true e o workspace
// for um git repo. O push para remote é responsabilidade do operador (ou
// de um job dedicado "git-sync") — deliberadamente fora do hot path.
package storage

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Dr0nj/regente-server/internal/domain"
	"gopkg.in/yaml.v3"
)

type FileStore struct {
	root      string
	gitCommit bool
	mu        sync.RWMutex
}

func NewFileStore(root string, gitCommit bool) *FileStore {
	return &FileStore{root: root, gitCommit: gitCommit}
}

func (s *FileStore) Root() string           { return s.root }
func (s *FileStore) definitionsDir() string { return filepath.Join(s.root, "definitions") }

// List caminha recursivamente em definitions/**/*.yaml.
func (s *FileStore) List() ([]domain.JobDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.JobDefinition{}
	err := filepath.WalkDir(s.definitionsDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == ".regente-folder.yaml" {
			return nil
		}
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var def domain.JobDefinition
		if err := yaml.Unmarshal(raw, &def); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		// Infer team from parent folder if not explicit in YAML
		if def.Team == "" {
			rel, _ := filepath.Rel(s.definitionsDir(), path)
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) > 1 {
				def.Team = parts[0]
			}
		}
		out = append(out, def)
		return nil
	})
	return out, err
}

// Save grava a definition e, opcionalmente, faz commit git.
func (s *FileStore) Save(def domain.JobDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if def.ID == "" || def.Team == "" {
		return fmt.Errorf("definition requires id and team")
	}
	dir := filepath.Join(s.definitionsDir(), def.Team)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, def.ID+".yaml")
	raw, err := yaml.Marshal(&def)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	if s.gitCommit {
		s.commit(path, fmt.Sprintf("regente: save %s/%s", def.Team, def.ID))
	}
	return nil
}

// Delete remove a definition do disco (e opcionalmente commita).
func (s *FileStore) Delete(team, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.definitionsDir(), team, id+".yaml")
	if err := os.Remove(path); err != nil {
		return err
	}
	if s.gitCommit {
		s.commit(path, fmt.Sprintf("regente: delete %s/%s", team, id))
	}
	return nil
}

// FolderInfo — folder com contagem de jobs (F11.6).
type FolderInfo struct {
	Name     string `json:"name"`
	JobCount int    `json:"jobCount"`
	Archived bool   `json:"archived,omitempty"`
}

// ListFolders retorna folders com contagem de jobs. Folders arquivados
// começam com ".archived-" prefix e ficam ocultos por padrão.
func (s *FileStore) ListFolders() ([]FolderInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.definitionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []FolderInfo{}, nil
		}
		return nil, err
	}
	out := []FolderInfo{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		archived := strings.HasPrefix(name, ".archived-")
		displayName := name
		if archived {
			displayName = strings.TrimPrefix(name, ".archived-")
		}
		// count yaml files (ignore folder marker stub)
		count := 0
		inner, err := os.ReadDir(filepath.Join(s.definitionsDir(), name))
		if err == nil {
			for _, f := range inner {
				if f.IsDir() {
					continue
				}
				n := f.Name()
				if n == ".regente-folder.yaml" {
					continue
				}
				if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
					count++
				}
			}
		}
		out = append(out, FolderInfo{Name: displayName, JobCount: count, Archived: archived})
	}
	return out, nil
}

// CreateFolder cria um folder vazio (útil para preparar times sem jobs).
func (s *FileStore) CreateFolder(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validFolderName(name); err != nil {
		return err
	}
	dir := filepath.Join(s.definitionsDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// sentinel file mantém o folder rastreável pelo git
	stub := filepath.Join(dir, ".regente-folder.yaml")
	if _, err := os.Stat(stub); os.IsNotExist(err) {
		_ = os.WriteFile(stub, []byte("# regente folder marker\nname: "+name+"\n"), 0o644)
		if s.gitCommit {
			s.commit(stub, fmt.Sprintf("regente: create folder %s", name))
		}
	}
	return nil
}

// RenameFolder move definitions/<old>/ → definitions/<new>/. Falha se <new>
// já existir.
func (s *FileStore) RenameFolder(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validFolderName(newName); err != nil {
		return err
	}
	oldPath := filepath.Join(s.definitionsDir(), oldName)
	newPath := filepath.Join(s.definitionsDir(), newName)
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("folder %q already exists", newName)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	// Rewrite team field em cada YAML do folder
	_ = filepath.WalkDir(newPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var def domain.JobDefinition
		if err := yaml.Unmarshal(raw, &def); err != nil {
			return nil
		}
		if def.Team == oldName {
			def.Team = newName
			out, err := yaml.Marshal(&def)
			if err == nil {
				_ = os.WriteFile(path, out, 0o644)
			}
		}
		return nil
	})
	if s.gitCommit {
		s.commit(newPath, fmt.Sprintf("regente: rename folder %s -> %s", oldName, newName))
	}
	return nil
}

// DeleteFolder remove um folder. Se force=false e o folder tiver jobs, falha.
func (s *FileStore) DeleteFolder(name string, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validFolderName(name); err != nil {
		return err
	}
	dir := filepath.Join(s.definitionsDir(), name)
	if !force {
		inner, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, f := range inner {
			if !f.IsDir() && (strings.HasSuffix(f.Name(), ".yaml") || strings.HasSuffix(f.Name(), ".yml")) && f.Name() != ".regente-folder.yaml" {
				return fmt.Errorf("folder %q has jobs (use force=true to delete anyway)", name)
			}
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if s.gitCommit {
		s.commit(dir, fmt.Sprintf("regente: delete folder %s", name))
	}
	return nil
}

// ArchiveFolder renomeia para ".archived-<name>" — mantém git history,
// oculta do UI padrão.
func (s *FileStore) ArchiveFolder(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validFolderName(name); err != nil {
		return err
	}
	old := filepath.Join(s.definitionsDir(), name)
	new := filepath.Join(s.definitionsDir(), ".archived-"+name)
	if _, err := os.Stat(new); err == nil {
		return fmt.Errorf("already archived")
	}
	if err := os.Rename(old, new); err != nil {
		return err
	}
	if s.gitCommit {
		s.commit(new, fmt.Sprintf("regente: archive folder %s", name))
	}
	return nil
}

func validFolderName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid folder name")
	}
	return nil
}

func (s *FileStore) commit(path, msg string) {
	cmds := [][]string{
		{"git", "add", path},
		{"git", "commit", "-m", msg},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = s.root
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "[storage] git %s: %s (err=%v)\n", strings.Join(c[1:], " "), string(out), err)
		}
	}
}
