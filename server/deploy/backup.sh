#!/usr/bin/env sh
# R6/DR — backup do state store do regente-server (SQLite ou Postgres).
#
# Uso:
#   REGENTE_DB_DRIVER=sqlite   REGENTE_DB=./regente.db                ./backup.sh [destino]
#   REGENTE_DB_DRIVER=postgres REGENTE_DB=postgres://u:p@host/db?... ./backup.sh [destino]
#
# Env extra:
#   REGENTE_BIN          caminho do binário (default ./regente-server) — usado no SQLite
#   REGENTE_BACKUP_KEEP  quantos backups manter (default 14)
#
# Rode via cron/systemd-timer/k8s CronJob (exemplos em docs/dr-backup.md).
set -eu

OUT="${1:-./backups}"
mkdir -p "$OUT"
TS="$(date +%Y%m%d-%H%M%S)"
DRIVER="${REGENTE_DB_DRIVER:-sqlite}"
DB="${REGENTE_DB:-./regente.db}"

case "$DRIVER" in
  postgres|postgresql|pg)
    DEST="$OUT/regente-$TS.dump"
    # Backup lógico consistente em custom format (restore seletivo com pg_restore).
    # Para PITR (point-in-time), combine com WAL archiving — ver docs/dr-backup.md.
    pg_dump "$DB" -Fc -f "$DEST"
    echo "[backup] postgres -> $DEST"
    ;;
  sqlite|sqlite3|"")
    DEST="$OUT/regente-$TS.db"
    # Snapshot ONLINE consistente via o próprio binário (VACUUM INTO, pure-Go).
    "${REGENTE_BIN:-./regente-server}" -db "$DB" -backup "$DEST"
    echo "[backup] sqlite -> $DEST"
    ;;
  *)
    echo "unknown driver: $DRIVER (use sqlite|postgres)" >&2
    exit 1
    ;;
esac

# Retenção: mantém os últimos N, apaga o resto.
KEEP="${REGENTE_BACKUP_KEEP:-14}"
ls -1t "$OUT"/regente-* 2>/dev/null | tail -n +"$((KEEP + 1))" | xargs -r rm -f
echo "[backup] OK (retention: last $KEEP in $OUT)"
