#!/usr/bin/env bash
# Instala o regente-agent como serviço systemd (Linux).
#
# Uso:
#   sudo SERVER=ws://host:8080/ws/agent TOKEN=rgta_xxx ID=meu-host \
#        CAPS=COMMAND,SCRIPT,HTTP USER=$USER ./install-linux.sh
#
# Pré: o binário regente-agent já compilado neste diretório (go build -o regente-agent .)
set -euo pipefail

SERVER="${SERVER:?defina SERVER=ws://host:8080/ws/agent}"
TOKEN="${TOKEN:?defina TOKEN=rgta_... (Settings → Agentes → Criar token)}"
ID="${ID:-$(hostname)}"
CAPS="${CAPS:-COMMAND,SCRIPT,HTTP}"
RUN_USER="${USER:-root}"

BIN_SRC="$(dirname "$0")/../regente-agent"
[ -x "$BIN_SRC" ] || { echo "binário não encontrado em $BIN_SRC — rode: (cd .. && go build -o regente-agent .)"; exit 1; }

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
echo "OK — regente-agent instalado e iniciado. Logs: journalctl -u regente-agent -f"
