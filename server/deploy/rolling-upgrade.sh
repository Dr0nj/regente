#!/usr/bin/env sh
# Ensaio descartável de drain com o MESMO binário, não certifica upgrade de versão.
# Pré-condições: PostgreSQL descartável, curl, cmp e portas livres.
set -eu
[ "${REGENTE_DISPOSABLE_DB:-}" = 1 ] || {
  echo "refusing: set REGENTE_DISPOSABLE_DB=1 only for a disposable PostgreSQL database" >&2
  exit 1
}
OLD="${REGENTE_OLD_BIN:-./regente-server}"
NEW="${REGENTE_NEW_BIN:-$OLD}"
[ -x "$OLD" ] && [ -x "$NEW" ] || { echo "both binaries must be executable" >&2; exit 1; }
cmp -s "$OLD" "$NEW" || {
  echo "refusing mixed binaries: this drill only qualifies SAME-BINARY process drain; see docs/upgrades.md" >&2
  exit 1
}
DSN="${REGENTE_PG_DSN:?set REGENTE_PG_DSN to a disposable PostgreSQL database}"
P_OLD="${PORT_OLD:-9080}"
P_NEW="${PORT_NEW:-9081}"
[ "$P_OLD" != "$P_NEW" ] || { echo "ports must differ" >&2; exit 1; }
WS="$(mktemp -d)"
OLD_PID=""
NEW_PID=""
cleanup() {
  for pid in "$OLD_PID" "$NEW_PID"; do
    [ -z "$pid" ] || { kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; }
  done
  rm -rf "$WS"
}
trap cleanup EXIT
trap 'exit 1' INT TERM
mkdir -p "$WS/old/definitions" "$WS/new/definitions"
leader_of() { curl -fsS "http://127.0.0.1:$1/readyz" 2>/dev/null | grep -o '"detail":"[a-z]*"' | head -1 | cut -d'"' -f4; }
wait_ready() {
  for i in $(seq 1 30); do
    kill -0 "$2" 2>/dev/null || return 1
    [ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$1/readyz" 2>/dev/null)" != 200 ] || return 0
    sleep 1
  done
  return 1
}
start_node() {
  "$1" -addr="127.0.0.1:$2" -db-driver=postgres -db="$DSN" -workspace="$WS/$3" \
    -git-source="" -github-repo="" -node-id="$3" -server-agent=false -selfmon=false \
    -scheduler=external -design-session-gc-tick-min=0 >"$WS/$3.log" 2>&1 &
  STARTED_PID=$!
}
# Nunca mata processos encontrados por porta. Só os PIDs criados por este ensaio.
for port in "$P_OLD" "$P_NEW"; do
  if curl -s --max-time 1 "http://127.0.0.1:$port/livez" >/dev/null 2>&1; then
    echo "refusing occupied HTTP port: $port" >&2
    exit 1
  fi
done
echo "[drain] SAME-BINARY disposable drill; no mixed-version or workload-continuity guarantee"
start_node "$OLD" "$P_OLD" old
OLD_PID=$STARTED_PID
wait_ready "$P_OLD" "$OLD_PID" || { echo "first test node did not start" >&2; exit 1; }
for i in $(seq 1 30); do
  [ "$(leader_of "$P_OLD")" != leader ] || break
  sleep 1
done
[ "$(leader_of "$P_OLD")" = leader ] || { echo "first node did not become leader" >&2; exit 1; }
start_node "$NEW" "$P_NEW" new
NEW_PID=$STARTED_PID
wait_ready "$P_NEW" "$NEW_PID" || { echo "second test node did not start" >&2; exit 1; }
kill "$OLD_PID"
wait "$OLD_PID" 2>/dev/null || true
OLD_PID=""
T0=$(date +%s)
for i in $(seq 1 30); do
  kill -0 "$NEW_PID" 2>/dev/null || { echo "surviving test process exited" >&2; exit 1; }
  curl -fsS "http://127.0.0.1:$P_NEW/livez" >/dev/null || { echo "liveness probe failed" >&2; exit 1; }
  if [ "$(leader_of "$P_NEW")" = leader ]; then
    echo "[drain] PASS: sampled liveness and leadership transfer in ~$(( $(date +%s) - T0 ))s"
    exit 0
  fi
  sleep 1
done
echo "FAILED: surviving node did not take leadership" >&2
exit 1
