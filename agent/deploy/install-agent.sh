#!/usr/bin/env bash
# install-agent.sh — instalador AMISTOSO do regente-agent (Linux e macOS).
#
# Baixa o binário da última release e registra como serviço 24/7 — systemd no Linux,
# launchd no macOS: inicia no boot, reinicia sozinho se cair, roda sem ninguém logado.
# NÃO precisa de Docker, Go nem runtime — é um binário estático.
#
# Interativo (pergunta servidor + token) — verificar o bootstrap antes de executar:
#   https://dr0nj.github.io/regente/authenticated-releases.html
#   sudo bash install-agent.sh
#
# Silencioso (frota / GPO-like) — passe por env:
#   sudo SERVER=wss://regente.suaempresa.com/ws/agent TOKEN=rgta_xxx bash install-agent.sh
#
# Variáveis: SERVER · TOKEN · ID(=hostname) · CAPS(=COMMAND,SCRIPT,HTTP) · RUN_USER
#            REGENTE_REPO · REGENTE_VERSION
set -euo pipefail

REPO="${REGENTE_REPO:-Dr0nj/regente}"
VERSION="${REGENTE_VERSION:-latest}"
[ "$(id -u)" = 0 ] || { echo "run as root:  sudo bash $0"; exit 1; }
command -v curl >/dev/null || { echo "'curl' is required"; exit 1; }

# OS/arch → nome do asset (bate com o release.yml: regente-agent_<os>_<arch>[.ext]).
OS="$(uname -s)"; MACH="$(uname -m)"
case "$MACH" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) echo "unsupported architecture: $MACH"; exit 1 ;; esac
case "$OS" in
  Linux)  GOOS=linux ;;
  Darwin) GOOS=darwin ;;
  *) echo "unsupported OS: $OS — on Windows use install-agent-windows.ps1"; exit 1 ;;
esac
ASSET="regente-agent_${GOOS}_${ARCH}"
if [ "$VERSION" = latest ]; then URL="https://github.com/$REPO/releases/latest/download/$ASSET"
else URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"; fi

# Config: env → prompt (só se houver terminal) → erro.
SERVER="${SERVER:-}"; TOKEN="${TOKEN:-}"
ID="${ID:-$(hostname)}"; CAPS="${CAPS:-COMMAND,SCRIPT,HTTP,EXECUTION_V2}"
RUN_USER="${RUN_USER:-${SUDO_USER:-root}}"
if [ -z "$SERVER" ] || [ -z "$TOKEN" ]; then
  if [ -t 0 ]; then
    [ -n "$SERVER" ] || read -rp  "Server URL (e.g. wss://regente.yourcompany.com/ws/agent): " SERVER
    [ -n "$TOKEN" ]  || { read -rsp "Agent token (Settings → Agents → Create token): " TOKEN; echo; }
    read -rp "Agent ID [$ID]: " _id; ID="${_id:-$ID}"
    read -rp "Capabilities [$CAPS]: " _caps; CAPS="${_caps:-$CAPS}"
  else
    echo "SERVER and/or TOKEN missing. Run it interactively (download it and 'sudo bash install-agent.sh')"
    echo "or pass them through env:  sudo SERVER=wss://.../ws/agent TOKEN=rgta_xxx bash install-agent.sh"
    exit 1
  fi
fi
[ -n "$SERVER" ] && [ -n "$TOKEN" ] || { echo "SERVER/TOKEN are empty"; exit 1; }

# Normaliza a URL: quem instala cola o endereço da UI (http://host:8080) muito mais
# vezes do que o endpoint do agente. Convertemos http->ws / https->wss e completamos
# o /ws/agent — sem isso o serviço sobe e fica reconectando em loop.
case "$SERVER" in
  http://*)       SERVER="ws://${SERVER#http://}" ;;
  https://*)      SERVER="wss://${SERVER#https://}" ;;
  ws://*|wss://*) : ;;
  *)              SERVER="ws://$SERVER" ;;
esac
case "$SERVER" in
  */ws/agent) : ;;
  */)         SERVER="${SERVER}ws/agent" ;;
  *)          SERVER="${SERVER}/ws/agent" ;;
esac
echo "== server: $SERVER"

# BEGIN RELEASE VERIFICATION — identity first, then exact bytes; no bypass.
verify_release() {
  local asset="$1" destination="$2"
  [ "$REPO" = Dr0nj/regente ] || { echo "Untrusted release repository." >&2; return 1; }
  command -v gh >/dev/null || { echo "Install a trusted GitHub CLI with attestation support first." >&2; return 1; }
  command -v python3 >/dev/null || { echo "python3 is required for release validation." >&2; return 1; }
  VERSION="${VERSION:-latest}"
  local base="https://github.com/$REPO/releases/download/$VERSION"
  [ "$VERSION" != latest ] || base="https://github.com/$REPO/releases/latest/download"
  local manifest="$TMP/release-manifest.json" signature="$TMP/release-manifest.sigstore.json"
  if [ -n "${REGENTE_MANIFEST:-}" ]; then
    cp "$REGENTE_MANIFEST" "$manifest" || return 1
    cp "${REGENTE_ATTESTATION:?Set REGENTE_ATTESTATION for an offline bundle}" "$signature" || return 1
  else
    curl -fsSL "$base/release-manifest.json" -o "$manifest" || return 1
    curl -fsSL "$base/release-manifest.sigstore.json" -o "$signature" || return 1
  fi
  # JSON não confiado nunca vira eval/comando/caminho.
  local fields tag sha ref
  fields="$(python3 - "$manifest" "$VERSION" <<'PY'
import json,re,sys
m=json.load(open(sys.argv[1]))
def require(ok,message):
    if not ok:raise SystemExit(message)
require(m['schema']==1 and m['repository']=='Dr0nj/regente' and m['workflow']=='.github/workflows/release.yml','Invalid release identity')
require(re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+',m['version']) and re.fullmatch(r'[0-9a-f]{40}',m['sourceSha']),'Invalid release version/source')
require(sys.argv[2]=='latest' or sys.argv[2]==m['version'],'Requested version differs')
require(m['sourceRef'] in ('refs/heads/main','refs/tags/'+m['version']),'Untrusted source ref')
print(m['version'],m['sourceSha'],m['sourceRef'])
PY
)" || return 1
  read -r tag sha ref <<< "$fields"
  gh attestation verify "$manifest" --bundle "$signature" --repo Dr0nj/regente \
    --signer-workflow Dr0nj/regente/.github/workflows/release.yml \
    --source-digest "$sha" --source-ref "$ref" --deny-self-hosted-runners >/dev/null || return 1
  if [ -n "${BUNDLE_LOCAL:-}" ]; then
    cp "$BUNDLE_LOCAL" "$destination" || return 1
  else
    curl -fSL "https://github.com/$REPO/releases/download/$tag/$asset" -o "$destination" || return 1
  fi
  python3 - "$manifest" "$asset" "$destination" <<'PY' || return 1
import hashlib,json,sys
from pathlib import Path
m=json.load(open(sys.argv[1])); a=m['assets'][sys.argv[2]]; p=Path(sys.argv[3])
if p.stat().st_size!=a['bytes'] or hashlib.sha256(p.read_bytes()).hexdigest()!=a['sha256']:
    raise SystemExit('Release payload integrity failed')
PY
  VERSION="$tag"
}
# END RELEASE VERIFICATION

# Download do binário.
BIN=/usr/local/bin/regente-agent
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
echo "== downloading $ASSET ($VERSION)..."
verify_release "$ASSET" "$TMP/agent"
install -m 0755 "$TMP/agent" "$BIN"

if [ "$GOOS" = linux ]; then
  # O token vai num EnvironmentFile 0600 (o agente lê REGENTE_TOKEN), não na
  # ExecStart: linha de comando é legível por qualquer usuário via `ps`.
  ENVFILE=/etc/regente/agent.env
  mkdir -p /etc/regente
  umask 077
  printf 'REGENTE_TOKEN=%s\n' "$TOKEN" > "$ENVFILE"
  chmod 0600 "$ENVFILE"
  install -d -m 0700 -o "$RUN_USER" /var/lib/regente-agent
  UNIT=/etc/systemd/system/regente-agent.service
  cat > "$UNIT" <<EOF
[Unit]
Description=Regente Agent (local executor)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
EnvironmentFile=$ENVFILE
ExecStart=$BIN -server $SERVER -id $ID -caps $CAPS -transport v2 -journal /var/lib/regente-agent/journal.db
Restart=always
RestartSec=5
User=$RUN_USER
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable regente-agent
  systemctl restart regente-agent
  echo ""
  echo "OK — regente-agent is up (systemd, Restart=always, starts at boot as '$RUN_USER')."
  # Prova que conectou de verdade, em vez de "instalou" e um loop de reconnect silencioso.
  sleep 3
  if systemctl is-active --quiet regente-agent; then
    echo "Service: active."
  else
    echo "Service: NOT running — most likely a wrong server URL or token. Last lines:"
    journalctl -u regente-agent -n 15 --no-pager || true
  fi
  echo "Logs:  journalctl -u regente-agent -f"
  echo "Stop:  sudo systemctl stop regente-agent"
else
  install -d -m 0700 /Library/Application\ Support/RegenteAgent
  PLIST=/Library/LaunchDaemons/com.regente.agent.plist
  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.regente.agent</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN</string>
    <string>-server</string><string>$SERVER</string>
    <string>-id</string><string>$ID</string>
    <string>-caps</string><string>$CAPS</string>
    <string>-transport</string><string>v2</string>
    <string>-journal</string><string>/Library/Application Support/RegenteAgent/journal.db</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict><key>REGENTE_TOKEN</key><string>$TOKEN</string></dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/regente-agent.log</string>
  <key>StandardErrorPath</key><string>/var/log/regente-agent.log</string>
</dict>
</plist>
EOF
  # 0600: o token mora aqui dentro; o launchd roda como root e lê assim mesmo.
  chown root:wheel "$PLIST"; chmod 0600 "$PLIST"
  # Recarrega (bootstrap moderno; cai pro load -w em macOS antigo).
  launchctl bootout system "$PLIST" 2>/dev/null || true
  launchctl bootstrap system "$PLIST" 2>/dev/null || launchctl load -w "$PLIST"
  echo ""
  echo "OK — regente-agent is up (launchd, KeepAlive, starts at boot)."
  echo "Logs:  tail -f /var/log/regente-agent.log"
  echo "Stop:  sudo launchctl bootout system $PLIST"
fi
echo "Check that agent '$ID' shows up online under Settings → Agents."
