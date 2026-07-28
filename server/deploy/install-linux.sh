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
[ -x "$BIN_SRC" ] || { echo "binary not found at $BIN_SRC — run: (cd .. && CGO_ENABLED=0 go build -o regente-server .)"; exit 1; }

install -m 0755 "$BIN_SRC" /usr/local/bin/regente-server
install -d -o "$RUN_USER" -g "$RUN_USER" "$DATA_DIR"

# server.env criado ANTES do bloco de UI (as duas formas mexem nele).
mkdir -p /etc/regente
if [ ! -f "$ENV_FILE" ]; then
  install -m 0640 "$HERE/server.env.example" "$ENV_FILE"
  echo "created $ENV_FILE (from the example) — EDIT it before exposing this in production."
fi

# --- V1: UI single-origin (opt-out via WITH_UI=0) --------------------------
# Dois layouts válidos, e o caminho é DIFERENTE em cada um:
#   • bundle de release: <bundle>/deploy/install-linux.sh + <bundle>/app/dist  → $HERE/../app/dist
#   • checkout do monorepo: server/deploy/install-linux.sh + app/dist na RAIZ  → $HERE/../../app/dist
# Só o primeiro era testado; do código-fonte o script caía silenciosamente em
# API-only (server sem UI). Agora procura nos dois, na ordem.
if [ -n "${SPA_DIR:-}" ]; then
  SPA_SRC="$SPA_DIR"
elif [ -f "$HERE/../app/dist/index.html" ]; then
  SPA_SRC="$HERE/../app/dist"
elif [ -f "$HERE/../../app/dist/index.html" ]; then
  SPA_SRC="$HERE/../../app/dist"
else
  SPA_SRC="$HERE/../app/dist"   # inexistente: cai no aviso abaixo com um caminho concreto
fi
SPA_DST="$DATA_DIR/app"
SPA_INSTALLED=0
case "$WITH_UI" in
  0|no|false|NO|FALSE)
    echo "WITH_UI=$WITH_UI — installing the API ONLY (the UI will not be served)."
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
      echo "UI installed at $SPA_DST (single-origin: UI+API+WS on the same port)."
      # REGENTE_SPA_DIR aponta pra UI (idempotente: descomenta/atualiza ou anexa).
      if grep -qE '^[[:space:]]*#?[[:space:]]*REGENTE_SPA_DIR=' "$ENV_FILE"; then
        sed -i -E "s|^[[:space:]]*#?[[:space:]]*REGENTE_SPA_DIR=.*|REGENTE_SPA_DIR=$SPA_DST|" "$ENV_FILE"
      else
        printf '\n# V1 — single-origin UI (set by the installer)\nREGENTE_SPA_DIR=%s\n' "$SPA_DST" >> "$ENV_FILE"
      fi
    else
      echo "WARNING: no built SPA found at $SPA_SRC — installing the API ONLY."
      echo "         To serve the UI too: (cd app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build), then reinstall"
      echo "         (or point it explicitly:  sudo SPA_DIR=/path/to/app/dist $0 ),"
      echo "         or use the release one-liner (install.sh), which already ships the UI bundled."
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
PORT="${ADDR##*:}"
# IP público/externo só para imprimir uma URL clicável (best-effort, sem rede externa).
HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
HOST_IP="${HOST_IP:-<this-host>}"

# Health check: prova que subiu de verdade, em vez de mandar o operador adivinhar.
sleep 1
HEALTH="unknown"
if command -v curl >/dev/null; then
  if curl -fsS --max-time 5 "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then HEALTH="ok"; else HEALTH="FAILED"; fi
fi

echo ""
echo "OK — regente-server installed and started (Restart=always) as user '$RUN_USER'."
echo "Health:  http://127.0.0.1:${PORT}/health -> ${HEALTH}"
if [ "$HEALTH" = "FAILED" ]; then
  echo "         it did not answer yet — check:  journalctl -u regente-server -n 50 --no-pager"
fi
echo ""
echo "== Next steps, in order"
if [ -x /usr/local/bin/regente-configure ]; then
  echo "  1) sudo regente-configure     # strong token, YOUR workspace repo + GitHub PAT"
else
  echo "  1) sudo \$EDITOR $ENV_FILE     # strong REGENTE_TOKEN, YOUR workspace repo"
fi
echo "  2) sudo systemctl restart regente-server"
echo "  3) open the port on the firewall (a cloud VM ALSO needs it in the security group):"
echo "       sudo ufw allow ${PORT}/tcp        # ufw"
echo "       sudo firewall-cmd --add-port=${PORT}/tcp --permanent && sudo firewall-cmd --reload   # firewalld"
if [ "$SPA_INSTALLED" = "1" ]; then
  echo "  4) open http://${HOST_IP}:${PORT}   (login: admin / admin — it forces a password change)"
else
  echo "  4) API only (no UI installed): curl http://${HOST_IP}:${PORT}/health"
fi
echo ""
echo "Logs:    journalctl -u regente-server -f"
echo "HTTPS/domain (a company-style public link): see deploy/vps/."
