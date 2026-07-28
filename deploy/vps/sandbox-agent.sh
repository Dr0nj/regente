#!/usr/bin/env bash
# V5 — sobe um AGENTE SANDBOX (container Docker isolado) como serviço systemd, pra
# amigos executarem jobs de verdade SEM tocar o host do VPS. Os jobs COMMAND/SCRIPT
# rodam DENTRO do container (sem mounts do host, não-root, cap-drop, limites) — o
# container é descartável; systemd supervisiona (Restart=always).
#
#   sudo AGENT_TOKEN=rgta_xxx ./sandbox-agent.sh
#
# Requisitos no VPS: Docker instalado e o CÓDIGO-FONTE do repo presente (o Dockerfile
# builda o Go DENTRO do Docker — não precisa de Go no host). Rode da raiz do repo ou
# de deploy/vps/.
#
# Variáveis:
#   AGENT_TOKEN=  (obrigatório) token do agente — "Settings → Agents → Create token"
#                 na UI, ou o REGENTE_TOKEN. Não use um token que você não queira
#                 dentro de um container.
#   AGENT_SERVER= ws://127.0.0.1:8080/ws/agent   (default; o server local)
#   AGENT_ID=     sandbox-<hostname>              (default)
#   AGENT_CAPS=   COMMAND,SCRIPT,HTTP             (default)
#   IMAGE=        regente-agent:sandbox           (tag da imagem buildada)
set -euo pipefail

[ "$(id -u)" = 0 ] || { echo "run as root (sudo)"; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "Docker not found — install it first (https://docs.docker.com/engine/install/)."; exit 1; }
DOCKER="$(command -v docker)"

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"   # deploy/vps -> raiz do repo
AGENT_SERVER="${AGENT_SERVER:-ws://127.0.0.1:8080/ws/agent}"
AGENT_TOKEN="${AGENT_TOKEN:-}"
AGENT_ID="${AGENT_ID:-sandbox-$(hostname)}"
AGENT_CAPS="${AGENT_CAPS:-COMMAND,SCRIPT,HTTP}"
IMAGE="${IMAGE:-regente-agent:sandbox}"

[ -n "$AGENT_TOKEN" ] || { echo "set AGENT_TOKEN=... (Settings → Agents → Create token, or the REGENTE_TOKEN)"; exit 1; }

# Build da imagem do agente (mesma da demo Windows; context = agent/, Go dentro do Docker).
DOCKERFILE="$REPO_ROOT/deploy/demo/Dockerfile.agent"
if [ -f "$DOCKERFILE" ] && [ -d "$REPO_ROOT/agent" ]; then
  echo "== building the agent image ($IMAGE)..."
  "$DOCKER" build -f "$DOCKERFILE" -t "$IMAGE" "$REPO_ROOT/agent"
elif "$DOCKER" image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "== using the image already available: $IMAGE (no source found to rebuild from)."
else
  echo "the agent source was not found and the image '$IMAGE' is missing."
  echo "clone the repo (git clone https://github.com/Dr0nj/regente.git) and run this again from there,"
  echo "or make the image '$IMAGE' available first (docker pull/tag)."
  exit 1
fi

# Env do serviço.
install -d /etc/regente
umask 077
cat > /etc/regente/sandbox-agent.env <<EOF
AGENT_SERVER=$AGENT_SERVER
AGENT_TOKEN=$AGENT_TOKEN
AGENT_ID=$AGENT_ID
AGENT_CAPS=$AGENT_CAPS
IMAGE=$IMAGE
EOF
chmod 0640 /etc/regente/sandbox-agent.env

# Unit systemd (systemd supervisiona o container; templata o caminho do docker).
UNIT=/etc/systemd/system/regente-agent-sandbox.service
sed -e "s#__DOCKER__#${DOCKER}#g" "$HERE/regente-agent-sandbox.service" > "$UNIT"

systemctl daemon-reload
systemctl enable --now regente-agent-sandbox

echo ""
echo "OK — sandbox agent is up (container 'regente-sandbox', isolated, supervised by systemd)."
echo "Fleet: look for agent '$AGENT_ID' under Settings → Agents (online)."
echo "Logs:  journalctl -u regente-agent-sandbox -f    |    docker logs -f regente-sandbox"
echo "Stop:  sudo systemctl stop regente-agent-sandbox"
echo ""
echo "Security: guest jobs run INSIDE the container (cap-drop ALL, no-new-privileges,"
echo "          CPU/RAM/PID limits, no host mounts). Networking is ON (HTTP jobs work);"
echo "          to cut it off, add --network none to the unit's ExecStart and restart."
