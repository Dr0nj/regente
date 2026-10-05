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
