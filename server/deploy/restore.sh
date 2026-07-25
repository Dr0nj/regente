#!/usr/bin/env sh
# R6/DR — restaura um backup do regente-server. PARE o servidor antes de restaurar.
#
# Uso:
#   REGENTE_DB_DRIVER=sqlite   REGENTE_DB=./regente.db                ./restore.sh /backups/regente-XXedb
#   REGENTE_DB_DRIVER=postgres REGENTE_DB=postgres://u:p@host/db?... ./restore.sh /backups/regente-XXdump
#
# Depois suba o servidor e confira GET /readyz (deve voltar 200 com db.ok=true).
set -eu

SRC="${1:?usage: restore.sh <backup-file>}"
DRIVER="${REGENTE_DB_DRIVER:-sqlite}"
DB="${REGENTE_DB:-./regente.db}"

[ -e "$SRC" ] || { echo "backup not found: $SRC" >&2; exit 1; }

case "$DRIVER" in
  postgres|postgresql|pg)
    # --clean --if-exists recria os objetos; o banco-alvo precisa existir.
    pg_restore --clean --if-exists --no-owner -d "$DB" "$SRC"
    echo "[restore] postgres <- $SRC"
    ;;
  sqlite|sqlite3|"")
    # Substitui o arquivo do state store pelo snapshot.
    cp "$SRC" "$DB"
    echo "[restore] sqlite <- $SRC (copied to $DB)"
    ;;
  *)
    echo "unknown driver: $DRIVER (use sqlite|postgres)" >&2
    exit 1
    ;;
esac

echo "[restore] done — start the server and check GET /readyz"
