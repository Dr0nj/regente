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
# Repositório do PRODUTO (não confundir com o repo de workspace do operador, que
# nunca tem default). Mesma convenção do install.sh: sobrescrevível por env.
REPO_URL="https://github.com/${REGENTE_REPO:-Dr0nj/regente}"

BIN_SRC="$HERE/../regente-server"
[ -x "$BIN_SRC" ] || { echo "binary not found at $BIN_SRC — run: (cd .. && CGO_ENABLED=0 go build -o regente-server .)"; exit 1; }

# Reinstalar POR CIMA é o caminho de upgrade suportado, e ele precisa ser detectado
# ANTES de qualquer coisa: trocar o arquivo do binário não troca o processo em
# memória. Guardamos o MainPID para provar, no fim, que o serviço realmente rodou
# a versão nova — "atualizei e não mudou nada" quase sempre é o processo antigo.
WAS_ACTIVE=0
OLD_PID=0
if systemctl is-active --quiet regente-server 2>/dev/null; then
  WAS_ACTIVE=1
  OLD_PID="$(systemctl show regente-server -p MainPID --value 2>/dev/null || echo 0)"
fi

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

# --- Arquivos de deploy da BORDA (nginx/TLS/sandbox) -----------------------
# Sem isto o deploy/vps/README.md manda `cp deploy/vps/nginx-regente.conf …` num
# caminho que NÃO existe depois do one-liner: o install.sh baixa o bundle num
# mktemp -d com `trap rm` e apaga tudo ao sair. Quem instalou pelo caminho
# recomendado ficava sem os arquivos que o próprio doc mandava copiar.
# Mesmos dois layouts da SPA, e o caminho é DIFERENTE em cada um:
#   • bundle de release:    <bundle>/deploy/vps        → $HERE/vps
#   • checkout do monorepo: <repo>/deploy/vps          → $HERE/../../deploy/vps
if [ -n "${DEPLOY_DIR:-}" ]; then
  VPS_SRC="$DEPLOY_DIR"
elif [ -f "$HERE/vps/nginx-regente.conf" ]; then
  VPS_SRC="$HERE/vps"
elif [ -f "$HERE/../../deploy/vps/nginx-regente.conf" ]; then
  VPS_SRC="$HERE/../../deploy/vps"
else
  VPS_SRC=""
fi
DEPLOY_DST="$DATA_DIR/deploy"
if [ -n "$VPS_SRC" ]; then
  rm -rf "$DEPLOY_DST/vps"
  install -d "$DEPLOY_DST/vps"
  cp -r "$VPS_SRC/." "$DEPLOY_DST/vps/"
  chmod 0755 "$DEPLOY_DST/vps"/*.sh 2>/dev/null || true
  echo "Edge deploy files installed at $DEPLOY_DST/vps (nginx + TLS + sandbox agent)."
else
  echo "NOTE: no deploy/vps found next to this script — the HTTPS/domain guide will not be installed."
  echo "      Get it from the repository when you need it:  $REPO_URL/tree/main/deploy/vps"
fi

UNIT=/etc/systemd/system/regente-server.service
sed -e "s#__USER__#${RUN_USER}#g" "$HERE/regente-server.service" > "$UNIT"

# V3 — config guiada disponível como `sudo regente-configure` (se o script veio junto).
if [ -f "$HERE/configure.sh" ]; then
  install -m 0755 "$HERE/configure.sh" /usr/local/bin/regente-configure
fi
# Upgrade em um comando: `sudo regente-update` (backup do banco + release nova +
# restart). Mesmo padrão do regente-configure — o script viaja no bundle.
if [ -f "$HERE/update.sh" ]; then
  install -m 0755 "$HERE/update.sh" /usr/local/bin/regente-update
fi

systemctl daemon-reload
systemctl enable regente-server >/dev/null 2>&1 || true
if [ "$WAS_ACTIVE" = 1 ]; then
  # `enable --now` NÃO serve para upgrade: o `start` de uma unit já ativa é no-op,
  # então o binário novo ficava no disco com o ANTIGO ainda rodando — e o operador
  # reinstalava, via tudo "ok" e nada mudado. Reinstalação por cima RESTARTA.
  echo "Existing service detected — restarting it so the new binary actually takes over."
  systemctl restart regente-server
else
  systemctl start regente-server
fi

ADDR="$(grep -E '^REGENTE_ADDR=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
ADDR="${ADDR:-:8080}"
PORT="${ADDR##*:}"
# Endereço para imprimir uma URL clicável (best-effort, sem rede externa).
# `hostname -I | awk '{print $1}'` pegava o PRIMEIRO endereço da lista, que NÃO é
# necessariamente o público: numa caixa com Docker a lista pode começar pela
# docker0 (172.17.0.1) e o instalador mandaria abrir uma URL que não existe.
# Ficamos só com IPv4 público; sem nenhum (VM atrás de NAT), NÃO inventamos —
# imprimimos um placeholder para o operador preencher.
public_ipv4() {
  local ip
  for ip in $(hostname -I 2>/dev/null); do
    case "$ip" in
      *:*) continue ;;  # IPv6: fora (a URL precisaria de colchetes e raramente é o caminho)
      10.*|127.*|169.254.*|192.168.*|172.1[6-9].*|172.2[0-9].*|172.3[01].*) continue ;;
      *) printf '%s' "$ip"; return 0 ;;
    esac
  done
  return 1
}
HOST_IP="$(public_ipv4 || true)"
HOST_IP="${HOST_IP:-<server-ip>}"

# Health check: prova que subiu de verdade, em vez de mandar o operador adivinhar.
sleep 1
HEALTH="unknown"
if command -v curl >/dev/null; then
  if curl -fsS --max-time 5 "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then HEALTH="ok"; else HEALTH="FAILED"; fi
fi

NEW_PID="$(systemctl show regente-server -p MainPID --value 2>/dev/null || echo 0)"

echo ""
if [ "$WAS_ACTIVE" = 1 ]; then
  echo "OK — regente-server UPGRADED in place (Restart=always) as user '$RUN_USER'."
  echo "Health:  http://127.0.0.1:${PORT}/health -> ${HEALTH}"
  if [ "$HEALTH" = "FAILED" ]; then
    echo "         it did not answer yet — check:  journalctl -u regente-server -n 50 --no-pager"
  fi
  # A prova de que a versão nova está NO AR: o processo é outro. Sem isto o
  # operador não tem como distinguir "atualizou" de "trocou o arquivo e pronto".
  if [ "$OLD_PID" != "0" ] && [ "$NEW_PID" != "0" ] && [ "$OLD_PID" != "$NEW_PID" ]; then
    echo "Process replaced: PID $OLD_PID -> $NEW_PID (the new binary is the one running)."
  elif [ "$OLD_PID" != "0" ] && [ "$OLD_PID" = "$NEW_PID" ]; then
    echo "WARNING: the process did NOT restart (still PID $OLD_PID) — the old binary is still running."
    echo "         run:  sudo systemctl restart regente-server"
  fi
  echo ""
  echo "Kept untouched: $ENV_FILE (token, GitOps, REGENTE_ADDR), the database and the"
  echo "workspace clone under $DATA_DIR, and anything in front of it (nginx, TLS certificates)."
  echo "Replaced: the binary, the UI, $DEPLOY_DST/vps and the systemd unit."
  echo "Schema migrations, if any, ran at boot. Agents are separate binaries — upgrade them"
  echo "on their own machines with agent/deploy/install-agent.sh."
  echo ""
  echo "Logs:    journalctl -u regente-server -f"
  exit 0
fi
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
if [ "$SPA_INSTALLED" = "1" ]; then
  echo "  3) FIRST ACCESS — without exposing the control plane. From YOUR machine:"
  echo "       ssh -L 18080:127.0.0.1:${PORT} <you>@${HOST_IP}"
  echo "     then open http://localhost:18080   (login: admin / admin — it forces a password change)"
else
  echo "  3) FIRST ACCESS (API only, no UI installed). From YOUR machine:"
  echo "       ssh -L 18080:127.0.0.1:${PORT} <you>@${HOST_IP}"
  echo "       curl http://localhost:18080/health"
fi
echo "  4) PUBLIC LINK — HTTPS with a real domain (this is the supported way to expose it):"
if [ -d "$DEPLOY_DST/vps" ]; then
  echo "       $DEPLOY_DST/vps/README.md   (nginx reverse proxy + Let's Encrypt + firewall)"
else
  echo "       $REPO_URL/tree/main/deploy/vps   (nginx reverse proxy + Let's Encrypt + firewall)"
fi
echo ""
echo "Publishing port ${PORT} straight to the internet is NOT the recommended path: it is plain"
echo "HTTP, so the admin password and REGENTE_TOKEN travel in the clear. If you still want it"
echo "temporarily, allow SSH in the SAME command — enabling ufw without it locks you out:"
echo "       sudo ufw allow OpenSSH && sudo ufw allow ${PORT}/tcp && sudo ufw enable   # ufw"
echo "       (OpenSSH is port 22 — if your sshd listens elsewhere, allow THAT port instead)"
echo "       sudo firewall-cmd --add-port=${PORT}/tcp --permanent && sudo firewall-cmd --reload   # firewalld"
echo "  (a cloud VM ALSO needs the port opened in its security group.)"
echo ""
echo "Logs:    journalctl -u regente-server -f"
