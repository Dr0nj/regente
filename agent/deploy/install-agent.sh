#!/usr/bin/env bash
# install-agent.sh — instalador AMISTOSO do regente-agent (Linux e macOS).
#
# Baixa o binário da última release e registra como serviço 24/7 — systemd no Linux,
# launchd no macOS: inicia no boot, reinicia sozinho se cair, roda sem ninguém logado.
# NÃO precisa de Docker, Go nem runtime — é um binário estático.
#
# Interativo (pergunta servidor + token) — baixe e rode:
#   curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install-agent.sh -o install-agent.sh
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
[ "$(id -u)" = 0 ] || { echo "rode como root:  sudo bash $0"; exit 1; }
command -v curl >/dev/null || { echo "'curl' é necessário"; exit 1; }

# OS/arch → nome do asset (bate com o release.yml: regente-agent_<os>_<arch>[.ext]).
OS="$(uname -s)"; MACH="$(uname -m)"
case "$MACH" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) echo "arquitetura não suportada: $MACH"; exit 1 ;; esac
case "$OS" in
  Linux)  GOOS=linux ;;
  Darwin) GOOS=darwin ;;
  *) echo "SO não suportado: $OS — no Windows use install-agent-windows.ps1"; exit 1 ;;
esac
ASSET="regente-agent_${GOOS}_${ARCH}"
if [ "$VERSION" = latest ]; then URL="https://github.com/$REPO/releases/latest/download/$ASSET"
else URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"; fi

# Config: env → prompt (só se houver terminal) → erro.
SERVER="${SERVER:-}"; TOKEN="${TOKEN:-}"
ID="${ID:-$(hostname)}"; CAPS="${CAPS:-COMMAND,SCRIPT,HTTP}"
RUN_USER="${RUN_USER:-${SUDO_USER:-root}}"
if [ -z "$SERVER" ] || [ -z "$TOKEN" ]; then
  if [ -t 0 ]; then
    [ -n "$SERVER" ] || read -rp  "URL do servidor (ex.: wss://regente.suaempresa.com/ws/agent): " SERVER
    [ -n "$TOKEN" ]  || { read -rsp "Token do agente (Settings → Agentes → Criar token): " TOKEN; echo; }
    read -rp "ID do agente [$ID]: " _id; ID="${_id:-$ID}"
    read -rp "Capabilities [$CAPS]: " _caps; CAPS="${_caps:-$CAPS}"
  else
    echo "faltou SERVER e/ou TOKEN. Rode interativo (baixe e 'sudo bash install-agent.sh')"
    echo "ou passe por env:  sudo SERVER=wss://.../ws/agent TOKEN=rgta_xxx bash install-agent.sh"
    exit 1
  fi
fi
[ -n "$SERVER" ] && [ -n "$TOKEN" ] || { echo "SERVER/TOKEN vazios"; exit 1; }

# Download do binário.
BIN=/usr/local/bin/regente-agent
TMP="$(mktemp 2>/dev/null || mktemp -t regente-agent)"; trap 'rm -f "$TMP"' EXIT
echo "== baixando $ASSET ($VERSION)..."
curl -fSL "$URL" -o "$TMP"
install -m 0755 "$TMP" "$BIN"

if [ "$GOOS" = linux ]; then
  UNIT=/etc/systemd/system/regente-agent.service
  cat > "$UNIT" <<EOF
[Unit]
Description=Regente Agent (executor local)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
ExecStart=$BIN -server $SERVER -token $TOKEN -id $ID -caps $CAPS
Restart=always
RestartSec=5
User=$RUN_USER
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now regente-agent
  echo ""
  echo "OK — regente-agent no ar (systemd, Restart=always, roda no boot como '$RUN_USER')."
  echo "Logs:    journalctl -u regente-agent -f"
  echo "Derruba: sudo systemctl stop regente-agent"
else
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
    <string>-token</string><string>$TOKEN</string>
    <string>-id</string><string>$ID</string>
    <string>-caps</string><string>$CAPS</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/regente-agent.log</string>
  <key>StandardErrorPath</key><string>/var/log/regente-agent.log</string>
</dict>
</plist>
EOF
  chown root:wheel "$PLIST"; chmod 0644 "$PLIST"
  # Recarrega (bootstrap moderno; cai pro load -w em macOS antigo).
  launchctl bootout system "$PLIST" 2>/dev/null || true
  launchctl bootstrap system "$PLIST" 2>/dev/null || launchctl load -w "$PLIST"
  echo ""
  echo "OK — regente-agent no ar (launchd, KeepAlive, roda no boot)."
  echo "Logs:    tail -f /var/log/regente-agent.log"
  echo "Derruba: sudo launchctl bootout system $PLIST"
fi
echo "Confira o agente '$ID' online em Settings → Agentes."
