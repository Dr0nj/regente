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

# BEGIN RELEASE VERIFICATION — identity first, then exact bytes; no bypass.
verify_release() {
  local asset="$1" destination="$2"
  [ "$REPO" = Dr0nj/regente ] || { echo "Untrusted release repository." >&2; return 1; }
  command -v gh >/dev/null || { echo "Install a trusted GitHub CLI with attestation support first." >&2; return 1; }
  command -v python3 >/dev/null || { echo "python3 is required for release validation." >&2; return 1; }
  VERSION="${VERSION:-latest}"
  local base="https://github.com/$REPO/releases/download/$VERSION"
  [ "$VERSION" != latest ] || base="https://github.com/$REPO/releases/latest/download"
  local manifest="$TMP/release-manifest.json" signature="$TMP/release-manifest.sigstore.json"
  if [ -n "${REGENTE_MANIFEST:-}" ]; then
    cp "$REGENTE_MANIFEST" "$manifest" || return 1
    cp "${REGENTE_ATTESTATION:?Set REGENTE_ATTESTATION for an offline bundle}" "$signature" || return 1
  else
    curl -fsSL "$base/release-manifest.json" -o "$manifest" || return 1
    curl -fsSL "$base/release-manifest.sigstore.json" -o "$signature" || return 1
  fi
  # JSON não confiado nunca vira eval/comando/caminho.
  local fields tag sha ref
  fields="$(python3 - "$manifest" "$VERSION" <<'PY'
import json,re,sys
m=json.load(open(sys.argv[1]))
def require(ok,message):
    if not ok:raise SystemExit(message)
require(m['schema']==1 and m['repository']=='Dr0nj/regente' and m['workflow']=='.github/workflows/release.yml','Invalid release identity')
require(re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+',m['version']) and re.fullmatch(r'[0-9a-f]{40}',m['sourceSha']),'Invalid release version/source')
require(sys.argv[2]=='latest' or sys.argv[2]==m['version'],'Requested version differs')
require(m['sourceRef'] in ('refs/heads/main','refs/tags/'+m['version']),'Untrusted source ref')
print(m['version'],m['sourceSha'],m['sourceRef'])
PY
)" || return 1
  read -r tag sha ref <<< "$fields"
  gh attestation verify "$manifest" --bundle "$signature" --repo Dr0nj/regente \
    --signer-workflow Dr0nj/regente/.github/workflows/release.yml \
    --source-digest "$sha" --source-ref "$ref" --deny-self-hosted-runners >/dev/null || return 1
  if [ -n "${BUNDLE_LOCAL:-}" ]; then
    cp "$BUNDLE_LOCAL" "$destination" || return 1
  else
    curl -fSL "https://github.com/$REPO/releases/download/$tag/$asset" -o "$destination" || return 1
  fi
  python3 - "$manifest" "$asset" "$destination" <<'PY' || return 1
import hashlib,json,sys
from pathlib import Path
m=json.load(open(sys.argv[1])); a=m['assets'][sys.argv[2]]; p=Path(sys.argv[3])
if p.stat().st_size!=a['bytes'] or hashlib.sha256(p.read_bytes()).hexdigest()!=a['sha256']:
    raise SystemExit('Release payload integrity failed')
PY
  VERSION="$tag"
}
# END RELEASE VERIFICATION

BUNDLE="regente-server_linux_${ARCH}.tar.gz"
if [ "$VERSION" = latest ]; then
  URL="https://github.com/$REPO/releases/latest/download/$BUNDLE"
else
  URL="https://github.com/$REPO/releases/download/$VERSION/$BUNDLE"
fi

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
echo "== Regente — downloading $BUNDLE ($VERSION) from $REPO ..."
verify_release "$BUNDLE" "$TMP/bundle.tar.gz"
tar -xzf "$TMP/bundle.tar.gz" -C "$TMP"
DIR="$TMP/regente-server_linux_${ARCH}"
[ -x "$DIR/regente-server" ] || { echo "invalid bundle: the regente-server binary was not found"; exit 1; }

# Delega pro installer systemd do bundle (que também instala a UI single-origin).
RUN_USER="${RUN_USER:-${SUDO_USER:-root}}" bash "$DIR/deploy/install-linux.sh"

echo ""
echo "  The workspace repository is YOURS and is different in every installation:"
echo "  create an EMPTY repository on GitHub (private is fine) and give it to regente-configure —"
echo "  the server writes the initial content into it and pushes on the first start."
echo "  Its clone lives in /var/lib/regente/workspace, so it survives restarts and upgrades."
echo "  For HTTPS/public links and agents on other machines, see deploy/ and the README."
