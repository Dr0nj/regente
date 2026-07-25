package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// runDatabase contra um SQLite real (driver pure-Go embutido). Cobre exec
// (rows affected) e query (render de linhas), auto-detecção e erros.
func TestDatabase_SQLiteExecAndQuery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")

	// DDL + INSERT (exec path) — auto-detectado como não-query.
	code, out := runDatabase(map[string]interface{}{
		"driver": "sqlite", "dsn": dbPath,
		"sql": "CREATE TABLE cargas(id INTEGER, nome TEXT)",
	}, 10, nil)
	if code != 0 {
		t.Fatalf("create table: code=%d out=%s", code, out)
	}
	code, out = runDatabase(map[string]interface{}{
		"driver": "sqlite", "dsn": dbPath,
		"sql": "INSERT INTO cargas(id,nome) VALUES(1,'ppbank'),(2,'base')",
	}, 10, nil)
	if code != 0 || !strings.Contains(out, "2 row(s) affected") {
		t.Fatalf("insert: code=%d out=%q", code, out)
	}

	// SELECT (query path) — auto-detectado, renderiza header + linhas + contagem.
	code, out = runDatabase(map[string]interface{}{
		"driver": "sqlite", "dsn": dbPath,
		"sql": "SELECT id, nome FROM cargas ORDER BY id",
	}, 10, nil)
	if code != 0 {
		t.Fatalf("select: code=%d out=%s", code, out)
	}
	for _, want := range []string{"id\tnome", "1\tppbank", "2\tbase", "(2 row(s))"} {
		if !strings.Contains(out, want) {
			t.Fatalf("select output faltou %q em:\n%s", want, out)
		}
	}
}

func TestDatabase_MaxRowsTruncates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = seed.Exec("CREATE TABLE n(i INTEGER)")
	for i := 0; i < 10; i++ {
		_, _ = seed.Exec("INSERT INTO n VALUES(?)", i)
	}
	seed.Close()

	code, out := runDatabase(map[string]interface{}{
		"driver": "sqlite", "dsn": dbPath,
		"sql": "SELECT i FROM n ORDER BY i", "maxRows": float64(3),
	}, 10, nil)
	if code != 0 {
		t.Fatalf("select: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "truncated at 3 rows") {
		t.Fatalf("esperava aviso de truncamento, veio:\n%s", out)
	}
}

func TestDatabase_MissingParams(t *testing.T) {
	if code, _ := runDatabase(map[string]interface{}{"driver": "sqlite"}, 5, nil); code != -1 {
		t.Fatal("faltando dsn/sql deveria falhar")
	}
	if code, out := runDatabase(map[string]interface{}{
		"driver": "oracle", "dsn": "x", "sql": "SELECT 1",
	}, 5, nil); code != -1 || !strings.Contains(out, "unsupported driver") {
		t.Fatalf("driver não suportado deveria falhar claramente, veio %d %q", code, out)
	}
}

func TestSQLLooksLikeQuery(t *testing.T) {
	yes := []string{"SELECT 1", "  select * from x", "WITH a AS(...)", "PRAGMA table_info(x)"}
	no := []string{"INSERT INTO x VALUES(1)", "UPDATE x SET y=1", "CREATE TABLE x(i INT)", "DELETE FROM x"}
	for _, s := range yes {
		if !sqlLooksLikeQuery(s) {
			t.Errorf("%q deveria ser query", s)
		}
	}
	for _, s := range no {
		if sqlLooksLikeQuery(s) {
			t.Errorf("%q NÃO deveria ser query", s)
		}
	}
}
