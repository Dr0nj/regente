package db

import (
	"fmt"
	"strings"
)

// OnlineBackup faz um backup ONLINE e consistente do state store em destPath, sem
// parar o servidor (R6/DR).
//
//   - SQLite: usa `VACUUM INTO` — snapshot transacionalmente consistente, pure-Go,
//     sem dependência externa. destPath deve ser um caminho de arquivo que ainda
//     não existe (o SQLite cria; se existir, falha — proposital, não sobrescreve).
//   - Postgres: backup in-process não é suportado (o estado mora num servidor PG
//     externo). Use `pg_dump` / PITR gerenciado — ver server/deploy/backup.sh e
//     docs/dr-backup.md. Retorna erro orientativo (não silencioso).
//
// Exposto também como modo one-shot do binário: `regente-server -backup <dest>`.
func OnlineBackup(d *DB, destPath string) error {
	if destPath == "" {
		return fmt.Errorf("backup: destino vazio")
	}
	switch d.dialect {
	case SQLite:
		// VACUUM INTO não aceita placeholder (`?`); o destino é literal na query.
		// destPath vem de operador (cron/CLI) — escapa aspas simples por segurança.
		safe := strings.ReplaceAll(destPath, "'", "''")
		if _, err := d.DB.Exec("VACUUM INTO '" + safe + "'"); err != nil {
			return fmt.Errorf("backup sqlite (VACUUM INTO): %w", err)
		}
		return nil
	case Postgres:
		return fmt.Errorf("backup in-process não suportado p/ postgres; use pg_dump " +
			"(server/deploy/backup.sh) ou PITR gerenciado (docs/dr-backup.md)")
	default:
		return fmt.Errorf("backup: dialeto %q não suportado", d.dialect)
	}
}
