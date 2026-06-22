#!/usr/bin/env bash
# Instala o regente-server como serviço systemd (Linux), com Restart=always.
# R1 — supervisor: o control plane volta sozinho se cair (crash, OOM, kill).
#
# Uso:
#   sudo USER=$USER ./install-linux.sh
#   # depois edite /etc/regente/server.env (REGENTE_TOKEN, REGENTE_DB, GitOps…)
#   sudo systemctl restart regente-server
#
# Pré: o binário regente-server já compilado no diretório pai
#      (cd .. && CGO_ENABLED=0 go build -o regente-server .)
set -euo pipefail

RUN_USER="${USER:-root}"
HERE="$(cd "$(dirname "$0")" && pwd)"
BIN_SRC="$HERE/../regente-server"
[ -x "$BIN_SRC" ] || { echo "binário não encontrado em $BIN_SRC — rode: (cd .. && CGO_ENABLED=0 go build -o regente-server .)"; exit 1; }

install -m 0755 "$BIN_SRC" /usr/local/bin/regente-server
install -d -o "$RUN_USER" -g "$RUN_USER" /var/lib/regente

mkdir -p /etc/regente
if [ ! -f /etc/regente/server.env ]; then
  install -m 0640 "$HERE/server.env.example" /etc/regente/server.env
  echo "criado /etc/regente/server.env (a partir do exemplo) — EDITE antes de usar em produção."
fi

UNIT=/etc/systemd/system/regente-server.service
sed -e "s#__USER__#${RUN_USER}#g" "$HERE/regente-server.service" > "$UNIT"

systemctl daemon-reload
systemctl enable --now regente-server
echo "OK — regente-server instalado e iniciado (Restart=always)."
echo "Config:  sudo \$EDITOR /etc/regente/server.env  &&  sudo systemctl restart regente-server"
echo "Logs:    journalctl -u regente-server -f"
