package db

import (
	"path/filepath"
	"testing"
)

// R6 — backup online (VACUUM INTO) gera um arquivo consistente e legível.
func TestOnlineBackup_SQLite(t *testing.T) {
	dir := t.TempDir()
	src, err := Open(SQLite, filepath.Join(dir, "src.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := Migrate(src); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := src.Exec(`INSERT INTO settings(key,value) VALUES('env_label','PROD')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dest := filepath.Join(dir, "backup.db")
	if err := OnlineBackup(src, dest); err != nil {
		t.Fatalf("backup: %v", err)
	}
	src.Close()

	// Reabre SÓ o backup e confirma o dado — prova que o snapshot é íntegro.
	bk, err := Open(SQLite, dest)
	if err != nil {
		t.Fatalf("reabrir backup: %v", err)
	}
	defer bk.Close()
	var v string
	if err := bk.QueryRow(`SELECT value FROM settings WHERE key='env_label'`).Scan(&v); err != nil {
		t.Fatalf("ler backup: %v", err)
	}
	if v != "PROD" {
		t.Fatalf("esperava PROD no backup, veio %q", v)
	}
}

// R6 — postgres in-process é rejeitado com erro orientativo (não silencioso).
func TestOnlineBackup_PostgresUnsupported(t *testing.T) {
	if err := OnlineBackup(&DB{dialect: Postgres}, "/tmp/x.dump"); err == nil {
		t.Fatal("esperava erro orientativo p/ postgres")
	}
}

func TestOnlineBackup_EmptyDest(t *testing.T) {
	if err := OnlineBackup(&DB{dialect: SQLite}, ""); err == nil {
		t.Fatal("esperava erro com destino vazio")
	}
}

// R4 — a config de runtime (settings) vive no DB durável e SOBREVIVE a um restart:
// escreve, fecha (simula shutdown), reabre o MESMO arquivo e relê. Em produção o
// equivalente é o volume persistente (SQLite) ou o Postgres externo.
func TestSettings_SurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")

	d1, err := Open(SQLite, path)
	if err != nil {
		t.Fatalf("open#1: %v", err)
	}
	if err := Migrate(d1); err != nil {
		t.Fatalf("migrate#1: %v", err)
	}
	if _, err := d1.Exec(
		`INSERT INTO settings(key,value) VALUES('env_label','PROD') ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
	); err != nil {
		t.Fatalf("escrever config: %v", err)
	}
	d1.Close() // shutdown

	d2, err := Open(SQLite, path) // restart: mesmo arquivo
	if err != nil {
		t.Fatalf("open#2: %v", err)
	}
	defer d2.Close()
	if err := Migrate(d2); err != nil { // boot roda migrate de novo (idempotente)
		t.Fatalf("migrate#2: %v", err)
	}
	var v string
	if err := d2.QueryRow(`SELECT value FROM settings WHERE key='env_label'`).Scan(&v); err != nil {
		t.Fatalf("config perdida no restart: %v", err)
	}
	if v != "PROD" {
		t.Fatalf("esperava PROD após restart, veio %q", v)
	}
}
