#!/usr/bin/env sh
# R6/DR — restaura um backup do regente-server. PARE o servidor antes de restaurar.
#
# Uso:
#   REGENTE_DB_DRIVER=sqlite   REGENTE_DB=./regente.db                ./restore.sh /backups/regente-XXedb
#   REGENTE_DB_DRIVER=postgres REGENTE_DB=postgres://u:p@host/db?... ./restore.sh /backups/regente-XXdump
#
# Alvo novo/isolado; restaurar drafts/config antes de subir o binário compatível.
set -eu

SRC="${1:?usage: restore.sh <backup-file>}"
DRIVER="${REGENTE_DB_DRIVER:-sqlite}"
DB="${REGENTE_DB:?set REGENTE_DB to a NEW isolated restore target}"
umask 077

[ -e "$SRC" ] || { echo "backup not found: $SRC" >&2; exit 1; }
echo "[restore] DATABASE ONLY. Stop all writers; restore draft files/configuration before application startup."

case "$DRIVER" in
  postgres|postgresql|pg)
    # Alvo deve ser uma base vazia criada pelo operador. Erro reverte o restore.
    pg_restore --exit-on-error --single-transaction --no-owner -d "$DB" "$SRC"
    echo "[restore] postgres <- $SRC"
    ;;
  sqlite|sqlite3|"")
    # Nunca mistura snapshot com DB existente ou WAL/SHM de outro recovery point.
    for target in "$DB" "$DB-wal" "$DB-shm"; do
      if [ -e "$target" ] || [ -L "$target" ]; then
        echo "refusing existing restore target or sidecar: $target" >&2
        exit 1
      fi
    done
    # noclobber também recusa criação concorrente do arquivo-alvo.
    (set -C; cat "$SRC" > "$DB")
    echo "[restore] sqlite <- $SRC (new target $DB)"
    ;;
  *)
    echo "unknown driver: $DRIVER (use sqlite|postgres)" >&2
    exit 1
    ;;
esac

echo "[restore] DB restored; restore draft paths/config first, then verify schema with a compatible binary and inspect recovered content."
