#!/usr/bin/env bash
# DOC-E: wrapper portátil para os perfis explícitos do verificador.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for runtime in python3 python; do
  if command -v "$runtime" >/dev/null 2>&1 &&
     "$runtime" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 10) else 1)' 2>/dev/null; then
    exec "$runtime" "$ROOT/scripts/verify.py" "$@"
  fi
done
echo "FAIL: Python 3.10+ is required. No verification gates ran." >&2
exit 1
