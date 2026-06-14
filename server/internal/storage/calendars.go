// Package storage — F14 Calendars: read/write calendars/<name>.yaml.
//
// Layout: <workspace>/calendars/<name>.yaml
//
// Cada calendar define dias elegíveis (businessDays + holidays + include/exclude).
package storage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Dr0nj/regente-server/internal/domain"
	"gopkg.in/yaml.v3"
)

type CalendarStore struct {
	root string
	mu   sync.RWMutex
}

func NewCalendarStore(root string) *CalendarStore {
	return &CalendarStore{root: root}
}

func (s *CalendarStore) dir() string {
	return filepath.Join(s.root, "calendars")
}

func (s *CalendarStore) path(name string) string {
	return filepath.Join(s.dir(), name+".yaml")
}

func (s *CalendarStore) List() ([]domain.Calendar, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Calendar{}
	err := filepath.WalkDir(s.dir(), func(path string, d fs.DirEntry, err error) error {
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
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		var cal domain.Calendar
		if uerr := yaml.Unmarshal(raw, &cal); uerr != nil {
			return fmt.Errorf("%s: %w", path, uerr)
		}
		if cal.Name == "" {
			cal.Name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		out = append(out, cal)
		return nil
	})
	return out, err
}

func (s *CalendarStore) Get(name string) (*domain.Calendar, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.path(name))
	if err != nil {
		return nil, err
	}
	var cal domain.Calendar
	if uerr := yaml.Unmarshal(raw, &cal); uerr != nil {
		return nil, uerr
	}
	if cal.Name == "" {
		cal.Name = name
	}
	return &cal, nil
}

func (s *CalendarStore) Save(cal domain.Calendar) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cal.Name == "" {
		return fmt.Errorf("calendar name required")
	}
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return err
	}
	raw, err := yaml.Marshal(&cal)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(cal.Name), raw, 0o644)
}

func (s *CalendarStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.path(name))
}
