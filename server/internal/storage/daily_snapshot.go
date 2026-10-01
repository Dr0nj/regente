package storage

import (
	"fmt"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v3"
	"path"
	"strings"
)

// DefinitionSnapshot lê objetos imutáveis sob o mesmo lock que escolhe HEAD.
// Edição/publicação do worktree depois desta leitura não mistura SHA e conteúdo.
func (g *GitOps) DefinitionSnapshot() ([]domain.JobDefinition, string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	repo, err := g.openRepo()
	if err != nil {
		return nil, "", err
	}
	head, err := repo.Head()
	if err != nil {
		return nil, "", err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, "", err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, "", err
	}
	defs := []domain.JobDefinition{}
	err = tree.Files().ForEach(func(f *object.File) error {
		if !strings.HasPrefix(f.Name, "definitions/") || path.Base(f.Name) == ".regente-folder.yaml" || (!strings.HasSuffix(f.Name, ".yaml") && !strings.HasSuffix(f.Name, ".yml")) {
			return nil
		}
		raw, e := f.Contents()
		if e != nil {
			return e
		}
		var d domain.JobDefinition
		if e := yaml.Unmarshal([]byte(raw), &d); e != nil {
			return fmt.Errorf("invalid definition %s: %w", f.Name, e)
		}
		if d.Team == "" {
			parts := strings.Split(strings.TrimPrefix(f.Name, "definitions/"), "/")
			if len(parts) > 1 {
				d.Team = parts[0]
			}
		}
		defs = append(defs, d)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return domain.NormalizeConditions(defs), head.Hash().String(), nil
}
