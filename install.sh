#!/usr/bin/env bash
# install.sh — one-liner de instalação do regente-server num VPS Linux (V2).
#
# Baixa o bundle "caixa única" da última release (binário do server + UI buildada
# + unit systemd) e instala como serviço supervisionado (Restart=always), servindo
# UI+API+WS numa origem só. NÃO precisa de Go nem Node no VPS.
#
# Recomendado (leia antes de rodar):
#   curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install.sh -o regente-install.sh
#   sudo bash regente-install.sh
#
# Direto:
#   curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install.sh | sudo bash
#
# Variáveis opcionais:
#   REGENTE_REPO=Dr0nj/regente          repo das releases
#   REGENTE_VERSION=latest|vX.Y.Z       versão a instalar
#   RUN_USER=<usuário>                  usuário do serviço (default: quem chamou o sudo)
set -euo pipefail

REPO="${REGENTE_REPO:-Dr0nj/regente}"
VERSION="${REGENTE_VERSION:-latest}"

[ "$(id -u)" = 0 ] || { echo "run as root:  sudo bash $0   (or:  curl … | sudo bash)"; exit 1; }
case "$(uname -s)" in
  Linux) : ;;
  *) echo "this installer is Linux-only (systemd). On Windows use deploy/install-windows.ps1"; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $(uname -m) (amd64/arm64 only)"; exit 1 ;;
esac
for c in curl tar systemctl; do
  command -v "$c" >/dev/null || { echo "'$c' is required and is not on the PATH"; exit 1; }
done

BUNDLE="regente-server_linux_${ARCH}.tar.gz"
if [ "$VERSION" = latest ]; then
  URL="https://github.com/$REPO/releases/latest/download/$BUNDLE"
else
  URL="https://github.com/$REPO/releases/download/$VERSION/$BUNDLE"
fi

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
echo "== Regente — downloading $BUNDLE ($VERSION) from $REPO ..."
curl -fSL "$URL" -o "$TMP/bundle.tar.gz"
tar -xzf "$TMP/bundle.tar.gz" -C "$TMP"
DIR="$TMP/regente-server_linux_${ARCH}"
[ -x "$DIR/regente-server" ] || { echo "invalid bundle: the regente-server binary was not found"; exit 1; }

# Delega pro installer systemd do bundle (que também instala a UI single-origin).
RUN_USER="${RUN_USER:-${SUDO_USER:-root}}" bash "$DIR/deploy/install-linux.sh"

echo ""
echo "== Next steps"
echo "  1) sudo \$EDITOR /etc/regente/server.env   # REPLACE REGENTE_TOKEN with a strong value; point GitOps at your repo"
echo "  2) sudo systemctl restart regente-server"
echo "  3) open http://<this-host>:8080  (login: admin / admin — you must change it on first use)"
echo "  For HTTPS/public links and agents on other machines, see deploy/ and the README."
