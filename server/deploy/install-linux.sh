#!/usr/bin/env bash
# Instala o regente-server como serviço systemd (Linux), com Restart=always.
# R1 — supervisor: o control plane volta sozinho se cair (crash, OOM, kill).
#
# Formas de subir (systemd):
#   • Só o server (API headless):   WITH_UI=0 sudo ./install-linux.sh
#   • Server + UI (single-origin):  sudo ./install-linux.sh   (default; precisa de app/dist)
#   • Server + agente:              suba o server aqui e rode agent/deploy/install-linux.sh
#
# V1 — com WITH_UI=1 (default), se houver um SPA buildado ao lado (../app/dist ou
#      $SPA_DIR), instala a UI junto e serve TUDO numa origem só (UI+API+WS numa porta).
#
# Uso (do código-fonte):
#   (cd .. && CGO_ENABLED=0 go build -o regente-server .)
#   (cd ../../app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build)   # p/ UI junto
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
WITH_UI="${WITH_UI:-1}"   # 0/no/false = instala SÓ a API (não serve a UI)
HERE="$(cd "$(dirname "$0")" && pwd)"
DATA_DIR=/var/lib/regente
ENV_FILE=/etc/regente/server.env

BIN_SRC="$HERE/../regente-server"
[ -x "$BIN_SRC" ] || { echo "binário não encontrado em $BIN_SRC — rode: (cd .. && CGO_ENABLED=0 go build -o regente-server .)"; exit 1; }

install -m 0755 "$BIN_SRC" /usr/local/bin/regente-server
install -d -o "$RUN_USER" -g "$RUN_USER" "$DATA_DIR"

# server.env criado ANTES do bloco de UI (as duas formas mexem nele).
mkdir -p /etc/regente
if [ ! -f "$ENV_FILE" ]; then
  install -m 0640 "$HERE/server.env.example" "$ENV_FILE"
  echo "criado $ENV_FILE (a partir do exemplo) — EDITE antes de expor em produção."
fi

# --- V1: UI single-origin (opt-out via WITH_UI=0) --------------------------
SPA_SRC="${SPA_DIR:-$HERE/../app/dist}"   # ../app/dist vale p/ checkout de código E p/ bundle de release
SPA_DST="$DATA_DIR/app"
SPA_INSTALLED=0
case "$WITH_UI" in
  0|no|false|NO|FALSE)
    echo "WITH_UI=$WITH_UI — instalando SÓ a API (UI não será servida)."
    # Garante API-only: comenta REGENTE_SPA_DIR se estiver ativo.
    sed -i -E 's|^([[:space:]]*)(REGENTE_SPA_DIR=.*)$|\1# \2|' "$ENV_FILE" || true
    ;;
  *)
    if [ -f "$SPA_SRC/index.html" ]; then
      rm -rf "$SPA_DST"
      install -d -o "$RUN_USER" -g "$RUN_USER" "$SPA_DST"
      cp -r "$SPA_SRC/." "$SPA_DST/"
      chown -R "$RUN_USER:$RUN_USER" "$SPA_DST"
      SPA_INSTALLED=1
      echo "UI instalada em $SPA_DST (single-origin: UI+API+WS na mesma porta)."
      # REGENTE_SPA_DIR aponta pra UI (idempotente: descomenta/atualiza ou anexa).
      if grep -qE '^[[:space:]]*#?[[:space:]]*REGENTE_SPA_DIR=' "$ENV_FILE"; then
        sed -i -E "s|^[[:space:]]*#?[[:space:]]*REGENTE_SPA_DIR=.*|REGENTE_SPA_DIR=$SPA_DST|" "$ENV_FILE"
      else
        printf '\n# V1 — UI single-origin (setado pelo install)\nREGENTE_SPA_DIR=%s\n' "$SPA_DST" >> "$ENV_FILE"
      fi
    else
      echo "AVISO: SPA buildada não encontrada em $SPA_SRC — instalando SÓ a API."
      echo "       Pra servir a UI junto: (cd app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build) e reinstale,"
      echo "       ou use o one-liner de release (install.sh), que já traz a UI empacotada."
    fi
    ;;
esac

UNIT=/etc/systemd/system/regente-server.service
sed -e "s#__USER__#${RUN_USER}#g" "$HERE/regente-server.service" > "$UNIT"

# V3 — config guiada disponível como `sudo regente-configure` (se o script veio junto).
if [ -f "$HERE/configure.sh" ]; then
  install -m 0755 "$HERE/configure.sh" /usr/local/bin/regente-configure
fi

systemctl daemon-reload
systemctl enable --now regente-server

ADDR="$(grep -E '^REGENTE_ADDR=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
ADDR="${ADDR:-:8080}"
echo ""
echo "OK — regente-server instalado e iniciado (Restart=always) como usuário '$RUN_USER'."
if [ "$SPA_INSTALLED" = "1" ]; then
  echo "UI:      http://<este-host>${ADDR}   (login inicial: admin / admin — troca obrigatória na 1ª vez)"
fi
if [ -x /usr/local/bin/regente-configure ]; then
  echo "Config:  sudo regente-configure   (guiado: token forte, GitHub PAT/repo, domínio)"
  echo "         ou edite à mão: sudo \$EDITOR $ENV_FILE"
else
  echo "Config:  sudo \$EDITOR $ENV_FILE   (REGENTE_TOKEN forte, GitOps, HTTPS…)"
fi
echo "         sudo systemctl restart regente-server"
echo "Logs:    journalctl -u regente-server -f"
echo "HTTPS/domínio (link público estilo empresa): ver deploy/vps/."
