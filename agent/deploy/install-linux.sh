#!/usr/bin/env bash
# Instala o regente-agent como serviço systemd (Linux).
#
# Uso:
#   sudo SERVER=ws://host:8080/ws/agent TOKEN=rgta_xxx ID=meu-host \
#        CAPS=COMMAND,SCRIPT,HTTP USER=$USER ./install-linux.sh
#
# Pré: o binário regente-agent já compilado neste diretório (go build -o regente-agent .)
set -euo pipefail

SERVER="${SERVER:?set SERVER=ws://host:8080/ws/agent}"
TOKEN="${TOKEN:?set TOKEN=rgta_... (Settings → Agents → Create token)}"
ID="${ID:-$(hostname)}"
CAPS="${CAPS:-COMMAND,SCRIPT,HTTP}"
RUN_USER="${RUN_USER:-${USER:-root}}"

# Mesma normalização do install-agent.sh: aceita o endereço da UI (http://host:8080)
# e o converte no endpoint do agente, em vez de deixar o serviço reconectando em loop.
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

BIN_SRC="$(dirname "$0")/../regente-agent"
[ -x "$BIN_SRC" ] || { echo "binary not found at $BIN_SRC — run: (cd .. && go build -o regente-agent .)"; exit 1; }

install -m 0755 "$BIN_SRC" /usr/local/bin/regente-agent

UNIT=/etc/systemd/system/regente-agent.service
sed -e "s#__SERVER__#${SERVER}#g" \
    -e "s#__TOKEN__#${TOKEN}#g" \
    -e "s#__ID__#${ID}#g" \
    -e "s#__CAPS__#${CAPS}#g" \
    -e "s#__USER__#${RUN_USER}#g" \
    "$(dirname "$0")/regente-agent.service" > "$UNIT"

systemctl daemon-reload
systemctl enable --now regente-agent
echo "OK — regente-agent installed and started (server: $SERVER). Logs: journalctl -u regente-agent -f"
sleep 3
if systemctl is-active --quiet regente-agent; then
  echo "Service: active — check that agent '$ID' shows up online under Settings → Agents."
else
  echo "Service: NOT running — most likely a wrong server URL or token. Last lines:"
  journalctl -u regente-agent -n 15 --no-pager || true
fi
