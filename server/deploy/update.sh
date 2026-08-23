#!/usr/bin/env bash
# update.sh — atualiza uma instalação systemd do regente-server: BACKUP do banco,
# baixa a release nova, instala por cima e reinicia. Instalado pelo
# install-linux.sh como `/usr/local/bin/regente-update`.
#
# Por que existe: reinstalar por cima já era o caminho de upgrade suportado (o
# install-linux.sh troca binário+UI e RESTARTA), mas o operador tinha de lembrar
# a URL do bundle, e nada tirava um snapshot do banco antes de trocar o binário —
# migração de schema roda no boot seguinte e não tem volta.
#
# Uso:
#   sudo regente-update                 # backup + última release
#   sudo regente-update v0.2.19         # versão específica (downgrade também)
#   sudo regente-update --no-backup     # pula o snapshot do banco
#   sudo regente-update -f              # reinstala a MESMA versão (UI/unit/deploy)
#
# Variáveis opcionais:
#   REGENTE_REPO=Dr0nj/regente          repo das releases
#   REGENTE_VERSION=vX.Y.Z              mesmo que passar a versão como argumento
#   REGENTE_BUNDLE=/caminho/b.tar.gz    instala ESTE bundle (máquina sem internet)
#   REGENTE_BACKUP_DIR=/var/lib/regente/backups
#   REGENTE_BACKUP_KEEP=14              quantos snapshots manter
set -euo pipefail

REPO="${REGENTE_REPO:-Dr0nj/regente}"
VERSION="${REGENTE_VERSION:-}"
BUNDLE_LOCAL="${REGENTE_BUNDLE:-}"
BACKUP=1
FORCE=0
BIN=/usr/local/bin/regente-server
ENV_FILE=/etc/regente/server.env
DATA_DIR=/var/lib/regente
BACKUP_DIR="${REGENTE_BACKUP_DIR:-$DATA_DIR/backups}"
BACKUP_KEEP="${REGENTE_BACKUP_KEEP:-14}"

# Help do OPERADOR: em inglês (I18N-5 — tudo que o usuário lê). O cabeçalho
# acima é comentário de desenvolvedor e segue em pt-BR, por isso não é reusado.
usage() {
  cat <<'EOF'
regente-update — update this regente-server installation (systemd, Linux).

Backs the database up, downloads the release, installs it over the current one
and restarts the service.

Usage:
  sudo regente-update                 backup + latest release
  sudo regente-update v0.2.19         a specific version (downgrade included)
  sudo regente-update --no-backup     skip the database snapshot
  sudo regente-update -f | --force    reinstall the SAME version (binary/UI/unit)
  sudo regente-update -h | --help     this help

Environment:
  REGENTE_REPO=Dr0nj/regente          release repository
  REGENTE_VERSION=vX.Y.Z              same as passing the version as an argument
  REGENTE_BUNDLE=/path/bundle.tar.gz  install THIS bundle (machine with no internet)
  REGENTE_BACKUP_DIR=/var/lib/regente/backups
  REGENTE_BACKUP_KEEP=14              how many snapshots to keep

Kept untouched: /etc/regente/server.env, the database and the workspace clone.
The previous binary stays at /usr/local/bin/regente-server.bak for rollback.
EOF
  exit "${1:-0}"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --no-backup)   BACKUP=0; shift ;;
    -f|--force)    FORCE=1; shift ;;
    -h|--help)     usage 0 ;;
    -*)            echo "unknown option: $1" >&2; usage 2 ;;
    *)             VERSION="$1"; shift ;;
  esac
done

[ "$(id -u)" = 0 ] || { echo "run as root:  sudo regente-update"; exit 1; }
case "$(uname -s)" in
  Linux) : ;;
  *) echo "this updater is Linux-only (systemd). On Windows use deploy/install-windows.ps1"; exit 1 ;;
esac
command -v systemctl >/dev/null || { echo "'systemctl' is required and is not on the PATH"; exit 1; }
command -v tar >/dev/null || { echo "'tar' is required and is not on the PATH"; exit 1; }
[ -x "$BIN" ] || {
  echo "no installation found at $BIN — this command UPDATES an existing install."
  echo "to install for the first time, see:  https://github.com/$REPO#-install"
  exit 1
}

CURRENT="$("$BIN" -version 2>/dev/null || echo unknown)"

# Usuário do serviço vem da UNIT INSTALADA, não do login de quem chamou. Sem isto
# um `sudo regente-update` feito de outra conta reescreveria a unit com outro
# User= e o serviço perderia acesso ao próprio $DATA_DIR.
UNIT_USER="$(systemctl show regente-server -p User --value 2>/dev/null || true)"
[ -n "$UNIT_USER" ] || UNIT_USER="${SUDO_USER:-root}"

# Roda como o User= da unit (o dono do banco). Importa no SQLite: abrir o banco
# como root pode criar -wal/-shm com dono root, e aí o serviço (que não é root)
# perde a escrita DEPOIS do update — falha que só aparece no boot seguinte.
run_as_service() {
  if [ -z "$UNIT_USER" ] || [ "$UNIT_USER" = root ]; then
    "$@"
  elif command -v runuser >/dev/null 2>&1; then
    runuser -u "$UNIT_USER" -- "$@"
  else
    su -s /bin/sh "$UNIT_USER" -c "$(printf '%q ' "$@")"
  fi
}

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

# ── 1. de onde vem a versão nova ─────────────────────────────────────────────
if [ -n "$BUNDLE_LOCAL" ]; then
  [ -f "$BUNDLE_LOCAL" ] || { echo "REGENTE_BUNDLE=$BUNDLE_LOCAL: file not found"; exit 1; }
  echo "== Regente — installing the local bundle $BUNDLE_LOCAL (no download)"
  cp "$BUNDLE_LOCAL" "$TMP/bundle.tar.gz"
else
  command -v curl >/dev/null || { echo "'curl' is required and is not on the PATH"; exit 1; }
  case "$(uname -m)" in
    x86_64|amd64)  ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "unsupported architecture: $(uname -m) (amd64/arm64 only)"; exit 1 ;;
  esac
  BUNDLE="regente-server_linux_${ARCH}.tar.gz"
  if [ -n "$VERSION" ] && [ "$VERSION" != latest ]; then
    TAG="$VERSION"
    URL="https://github.com/$REPO/releases/download/$TAG/$BUNDLE"
  else
    # A tag da `latest` sai do redirect — sem isso não dá pra dizer "já está
    # atualizado" sem baixar 40 MB para descobrir.
    TAG="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null | sed 's#.*/##')"
    URL="https://github.com/$REPO/releases/latest/download/$BUNDLE"
  fi
  if [ -n "$TAG" ] && [ "$TAG" = "$CURRENT" ] && [ "$FORCE" = 0 ]; then
    echo "already on $CURRENT — nothing to do."
    echo "(reinstall the same version anyway with:  sudo regente-update -f)"
    exit 0
  fi
  echo "== Regente — downloading $BUNDLE (${TAG:-latest}) from $REPO ..."
  curl -fSL "$URL" -o "$TMP/bundle.tar.gz"
fi

tar -xzf "$TMP/bundle.tar.gz" -C "$TMP"
DIR="$(find "$TMP" -maxdepth 1 -type d -name 'regente-server_linux_*' | head -1)"
[ -n "$DIR" ] && [ -x "$DIR/regente-server" ] || { echo "invalid bundle: the regente-server binary was not found"; exit 1; }
[ -f "$DIR/deploy/install-linux.sh" ] || { echo "invalid bundle: deploy/install-linux.sh was not found"; exit 1; }

NEWVER="$("$DIR/regente-server" -version 2>/dev/null || echo unknown)"
if [ "$NEWVER" = "$CURRENT" ] && [ "$FORCE" = 0 ]; then
  echo "already on $CURRENT — nothing to do."
  echo "(reinstall the same version anyway with:  sudo regente-update -f)"
  exit 0
fi
echo "== version: $CURRENT -> $NEWVER"

# ── 2. backup do banco (default; --no-backup pula) ───────────────────────────
# Snapshot ANTES de trocar o binário: as migrações de schema rodam no boot
# seguinte e não têm desfazer. Feito com o binário ATUAL, que é quem conhece o
# schema atual.
if [ "$BACKUP" = 1 ]; then
  DRIVER="$(grep -E '^REGENTE_DB_DRIVER=' "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2- || true)"
  DB="$(grep -E '^REGENTE_DB=' "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2- || true)"
  DRIVER="${DRIVER:-sqlite}"
  DB="${DB:-$DATA_DIR/regente.db}"
  TS="$(date +%Y%m%d-%H%M%S)"
  install -d -m 0750 -o "$UNIT_USER" -g "$UNIT_USER" "$BACKUP_DIR" 2>/dev/null || mkdir -p "$BACKUP_DIR"
  case "$DRIVER" in
    postgres|postgresql|pg)
      command -v pg_dump >/dev/null || {
        echo "REGENTE_DB_DRIVER=$DRIVER but 'pg_dump' is not on the PATH."
        echo "back the database up your own way and re-run with:  sudo regente-update --no-backup"
        exit 1
      }
      DEST="$BACKUP_DIR/regente-$TS.dump"
      pg_dump "$DB" -Fc -f "$DEST"
      ;;
    sqlite|sqlite3|"")
      DEST="$BACKUP_DIR/regente-$TS.db"
      # Snapshot ONLINE (VACUUM INTO): não precisa parar o serviço.
      run_as_service "$BIN" -db "$DB" -db-driver sqlite -backup "$DEST" >/dev/null
      ;;
    *)
      echo "unknown REGENTE_DB_DRIVER=$DRIVER (sqlite|postgres) — re-run with --no-backup if that is on purpose"
      exit 1
      ;;
  esac
  echo "== backup: $DEST  ($(du -h "$DEST" 2>/dev/null | cut -f1))"
  # Retenção: mantém os últimos N, apaga o resto.
  ls -1t "$BACKUP_DIR"/regente-* 2>/dev/null | tail -n +"$((BACKUP_KEEP + 1))" | xargs -r rm -f || true
else
  echo "== backup: SKIPPED (--no-backup)"
fi

# Binário antigo guardado ao lado: rollback é um cp, sem depender de download.
cp -a "$BIN" "$BIN.bak" 2>/dev/null || true

# ── 3. instala por cima (troca binário+UI, daemon-reload, RESTART, health) ────
RUN_USER="$UNIT_USER" bash "$DIR/deploy/install-linux.sh"

INSTALLED="$("$BIN" -version 2>/dev/null || echo unknown)"
echo ""
echo "== regente-update done: $CURRENT -> $INSTALLED"
if [ "$BACKUP" = 1 ]; then
  echo "   database snapshot: ${DEST:-none}"
fi
echo "   rollback (previous binary kept):"
echo "     sudo install -m 0755 $BIN.bak $BIN && sudo systemctl restart regente-server"
echo "   agents are separate binaries — update them on their own machines with install-agent.sh."
