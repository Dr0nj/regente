#!/usr/bin/env bash
# Instala o regente-server como serviço systemd (Linux), com Restart=always.
# R1 — supervisor: o control plane volta sozinho se cair (crash, OOM, kill).
# V1 — se houver um SPA buildado ao lado (../app/dist ou $SPA_DIR), instala a UI
#      junto e serve TUDO numa origem só (UI+API+WS na mesma porta, sem CORS).
#
# Uso (do código-fonte):
#   (cd .. && CGO_ENABLED=0 go build -o regente-server .)
#   (cd ../../app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build)   # opcional: UI junto
#   sudo ./install-linux.sh
#   sudo $EDITOR /etc/regente/server.env      # REGENTE_TOKEN forte, GitOps…
#   sudo systemctl restart regente-server
#
# Do bundle de release (V2): os arquivos já vêm no lugar certo; o one-liner
# install.sh baixa o bundle e chama este script — nenhum toolchain no VPS.
set -euo pipefail

# Usuário do serviço: com `curl | sudo bash` o SUDO_USER é quem chamou; sem sudo
# cai no USER; por último, root. (Passável explicitamente via RUN_USER=.)
RUN_USER="${RUN_USER:-${SUDO_USER:-${USER:-root}}}"
HERE="$(cd "$(dirname "$0")" && pwd)"
DATA_DIR=/var/lib/regente

BIN_SRC="$HERE/../regente-server"
[ -x "$BIN_SRC" ] || { echo "binário não encontrado em $BIN_SRC — rode: (cd .. && CGO_ENABLED=0 go build -o regente-server .)"; exit 1; }

install -m 0755 "$BIN_SRC" /usr/local/bin/regente-server
install -d -o "$RUN_USER" -g "$RUN_USER" "$DATA_DIR"

# --- V1: UI single-origin --------------------------------------------------
# Procura a SPA buildada: $SPA_DIR explícito, senão ../app/dist — que vale tanto
# pro checkout de código quanto pro bundle de release (que espelha o layout do repo).
SPA_SRC="${SPA_DIR:-$HERE/../app/dist}"
SPA_DST="$DATA_DIR/app"
SPA_INSTALLED=0
if [ -f "$SPA_SRC/index.html" ]; then
  rm -rf "$SPA_DST"
  install -d -o "$RUN_USER" -g "$RUN_USER" "$SPA_DST"
  cp -r "$SPA_SRC/." "$SPA_DST/"
  chown -R "$RUN_USER:$RUN_USER" "$SPA_DST"
  SPA_INSTALLED=1
  echo "UI instalada em $SPA_DST (single-origin: UI+API+WS na mesma porta)."
else
  echo "AVISO: SPA buildada não encontrada em $SPA_SRC — instalando SÓ a API."
  echo "       Pra servir a UI junto: (cd app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build) e reinstale,"
  echo "       ou use o one-liner de release (install.sh), que já traz a UI empacotada."
fi

mkdir -p /etc/regente
if [ ! -f /etc/regente/server.env ]; then
  install -m 0640 "$HERE/server.env.example" /etc/regente/server.env
  echo "criado /etc/regente/server.env (a partir do exemplo) — EDITE antes de expor em produção."
fi

# REGENTE_SPA_DIR aponta pra UI instalada (idempotente: descomenta/atualiza ou anexa).
if [ "$SPA_INSTALLED" = "1" ]; then
  if grep -qE '^[[:space:]]*#?[[:space:]]*REGENTE_SPA_DIR=' /etc/regente/server.env; then
    sed -i -E "s|^[[:space:]]*#?[[:space:]]*REGENTE_SPA_DIR=.*|REGENTE_SPA_DIR=$SPA_DST|" /etc/regente/server.env
  else
    printf '\n# V1 — UI single-origin (setado pelo install)\nREGENTE_SPA_DIR=%s\n' "$SPA_DST" >> /etc/regente/server.env
  fi
fi

UNIT=/etc/systemd/system/regente-server.service
sed -e "s#__USER__#${RUN_USER}#g" "$HERE/regente-server.service" > "$UNIT"

systemctl daemon-reload
systemctl enable --now regente-server

ADDR="$(grep -E '^REGENTE_ADDR=' /etc/regente/server.env | tail -1 | cut -d= -f2-)"
ADDR="${ADDR:-:8080}"
echo ""
echo "OK — regente-server instalado e iniciado (Restart=always) como usuário '$RUN_USER'."
if [ "$SPA_INSTALLED" = "1" ]; then
  echo "UI:      http://<este-host>${ADDR}   (login inicial: admin / admin — troca obrigatória na 1ª vez)"
fi
echo "Config:  sudo \$EDITOR /etc/regente/server.env   (REGENTE_TOKEN forte, GitOps, HTTPS…)"
echo "         sudo systemctl restart regente-server"
echo "Logs:    journalctl -u regente-server -f"
