package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// O índice é por leitura: não compartilha definitions mutáveis entre leitores
// e não pode mascarar uma alteração externa, inclusive com tamanho/mtime iguais.
func TestConditionIndexConcurrentReadsAndExternalEdit(t *testing.T) {
	s := NewFileStore(t.TempDir(), false)
	for _, d := range []domain.JobDefinition{
		{ID: "producer", Team: "team", ConditionsOutAdd: []string{"OLD"}},
		{ID: "consumer", Team: "team", ConditionsIn: []string{"OLD"}},
	} {
		if err := s.Save(d); err != nil {
			t.Fatal(err)
		}
	}
	const readers = 10
	results := make([][]domain.JobDefinition, readers)
	errs := make([]error, readers)
	var wg sync.WaitGroup
	for i := range readers {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], errs[i] = s.List() }()
	}
	wg.Wait()
	for i := range readers {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if len(results[i]) != 2 || len(results[i][0].Upstream) != 1 || results[i][0].Upstream[0].From != "producer" {
			t.Fatalf("leitor %d não recebeu a topologia esperada", i)
		}
	}
	results[0][0].Upstream[0].From = "changed"
	if results[1][0].Upstream[0].From != "producer" {
		t.Fatal("leitores compartilham estado mutável")
	}
	path := filepath.Join(s.definitionsDir(), "team", "producer.yaml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Alteração direta mantém extensão, tamanho e timestamp, mas troca o produtor.
	for i := 0; i+3 <= len(raw); i++ {
		if string(raw[i:i+3]) == "OLD" {
			copy(raw[i:i+3], "NEW")
		}
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].Upstream) != 0 {
		t.Fatal("índice anterior sobreviveu à alteração externa")
	}
}
