#!/usr/bin/env bash
# Safe guided setup. Existing deployments are never overwritten or restarted.
set -euo pipefail
ENV_FILE="${1:-/etc/regente/server.env}"
if [ -e "$ENV_FILE" ]; then
  echo "Existing configuration preserved: $ENV_FILE"
  echo "Read the bootstrap password privately, log in, then remove REGENTE_BOOTSTRAP_PASSWORD."
  echo "For a fresh configuration run: regente-server -init-config /path/to/NEW-server.env"
  echo "Configure GitOps in Settings and review docs/production-profile.md before proxy/TLS exposure."
  exit 0
fi
umask 077
exec regente-server -init-config "$ENV_FILE"
