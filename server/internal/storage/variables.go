// Package storage \u2014 F18 Variables (global scope) com persist\u00eancia em SQLite.
//
// Escopos suportados aqui: apenas Global (key/value). Folder/job scopes vivem
// no JobDefinition.Variables (job) e ainda n\u00e3o existem para folder.
//
// O store mant\u00e9m cache em mem\u00f3ria (load no boot via Reload) e atualiza o DB
// em cada Set/Delete; assim o scheduler l\u00ea sem hit em DB no hot path.
package storage

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
)

// Variable \u2014 uma entrada de var global.
type Variable struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
}

// VariableStore \u2014 cache + persist\u00eancia para variables globais.
type VariableStore struct {
	db    *db.DB
	mu    sync.RWMutex
	cache map[string]Variable
}

// NewVariableStore abre/migra a tabela e carrega cache.
func NewVariableStore(db *db.DB) (*VariableStore, error) {
	vs := &VariableStore{db: db, cache: map[string]Variable{}}
	if err := vs.Reload(); err != nil {
		return nil, err
	}
	return vs, nil
}

// Reload recarrega cache do DB.
func (s *VariableStore) Reload() error {
	rows, err := s.db.Query(`SELECT name, value, updated_at, COALESCE(updated_by, '') FROM variables`)
	if err != nil {
		return err
	}
	defer rows.Close()
	next := map[string]Variable{}
	for rows.Next() {
		var v Variable
		if err := rows.Scan(&v.Name, &v.Value, &v.UpdatedAt, &v.UpdatedBy); err != nil {
			return err
		}
		next[v.Name] = v
	}
	s.mu.Lock()
	s.cache = next
	s.mu.Unlock()
	return nil
}

// List \u2014 snapshot ordenado por nome.
func (s *VariableStore) List() []Variable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Variable, 0, len(s.cache))
	for _, v := range s.cache {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get \u2014 single variable.
func (s *VariableStore) Get(name string) (Variable, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.cache[name]
	return v, ok
}

// Snapshot \u2014 mapa name\u2192value para inje\u00e7\u00e3o no VarContext.
func (s *VariableStore) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.cache))
	for k, v := range s.cache {
		out[k] = v.Value
	}
	return out
}

// Set \u2014 upsert (admin write). actor pode ser vazio.
func (s *VariableStore) Set(name, value, actor string) (Variable, error) {
	if name == "" {
		return Variable{}, fmt.Errorf("name required")
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(
		`INSERT INTO variables(name, value, updated_at, updated_by) VALUES(?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		name, value, now, actor,
	); err != nil {
		return Variable{}, err
	}
	v := Variable{Name: name, Value: value, UpdatedAt: now, UpdatedBy: actor}
	s.mu.Lock()
	s.cache[name] = v
	s.mu.Unlock()
	return v, nil
}

// Delete \u2014 remove variable. N\u00e3o falha se ausente (idempotente).
func (s *VariableStore) Delete(name string) error {
	if _, err := s.db.Exec(`DELETE FROM variables WHERE name = ?`, name); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cache, name)
	s.mu.Unlock()
	return nil
}
