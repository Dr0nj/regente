#!/usr/bin/env bash
# verify.sh — roda o equivalente LOCAL da CI (.github/workflows/ci.yml):
# server (build+vet+test) · agent (build+test) · app (build). Para na 1ª falha.
#
# Uso:  bash scripts/verify.sh        (da raiz ou de qualquer lugar)
# Slash: /verify
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "▶ server — build + vet + test"
( cd server && go build ./... && go vet ./... && go test ./... )

echo "▶ agent — build + test"
( cd agent && go build ./... && go test ./... )

echo "▶ app — build (tsc -b && vite build)"
# usa as deps já instaladas; se quebrar com 'Cannot find native binding',
# rode antes: ( cd app && rm -rf node_modules package-lock.json && npm install )
( cd app && npm run build )

echo "✅ verify OK — all green, same as the CI will be."
