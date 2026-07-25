#!/usr/bin/env sh
# Rolling upgrade zero-downtime — demonstra/valida a troca de versão sem derrubar a API.
# Aproveita o leader election (advisory lock): sobe o nó NOVO como follower, espera ficar
# READY, drena o nó ANTIGO (líder) e mede em quanto tempo o novo assume — enquanto a API
# segue respondendo o tempo todo (follower serve API; só o scheduler pausa ~4s, idempotente).
#
# Uso:
#   REGENTE_OLD_BIN=./regente-server-vN  REGENTE_NEW_BIN=./regente-server-vN1 \
#   REGENTE_PG_DSN="postgres://u:p@host:5432/db?sslmode=disable" ./rolling-upgrade.sh
#
# (OLD_BIN e NEW_BIN podem ser o MESMO binário só para exercitar o procedimento de drain.)
set -eu

OLD="${REGENTE_OLD_BIN:-./regente-server}"
NEW="${REGENTE_NEW_BIN:-$OLD}"
DSN="${REGENTE_PG_DSN:?set REGENTE_PG_DSN (postgres://...) — a rolling upgrade needs shared state}"
P_OLD="${PORT_OLD:-9080}"
P_NEW="${PORT_NEW:-9081}"
WS="$(mktemp -d)"
mkdir -p "$WS/old/definitions" "$WS/new/definitions"

leader_of() { curl -s "http://127.0.0.1:$1/readyz" 2>/dev/null | grep -o '"detail":"[a-z]*"' | head -1 | cut -d'"' -f4; }
wait_ready() { for i in $(seq 1 20); do [ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$1/readyz" 2>/dev/null)" = "200" ] && return 0; sleep 1; done; return 1; }
api_up()  { [ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$1/livez" 2>/dev/null)" = "200" ]; }
kill_port() { p=$(netstat -ano 2>/dev/null | grep LISTENING | grep ":$1 " | awk '{print $NF}' | head -1); [ -n "${p:-}" ] && { taskkill //PID "$p" //F >/dev/null 2>&1 || kill -9 "$p" 2>/dev/null; }; }

echo "[upgrade] 1) starting the OLD node (:$P_OLD) — it becomes leader"
nohup "$OLD" -addr=":$P_OLD" -db-driver=postgres -db="$DSN" -workspace="$WS/old" -git-source="" -github-repo="" -node-id=old >"$WS/old.log" 2>&1 &
wait_ready "$P_OLD" || { echo "the old node did not start"; exit 1; }
echo "[upgrade]    old=$(leader_of "$P_OLD")"

echo "[upgrade] 2) starting the NEW node (:$P_NEW) — joins as follower (lock taken)"
nohup "$NEW" -addr=":$P_NEW" -db-driver=postgres -db="$DSN" -workspace="$WS/new" -git-source="" -github-repo="" -node-id=new >"$WS/new.log" 2>&1 &
wait_ready "$P_NEW" || { echo "the new node never became READY"; exit 1; }
echo "[upgrade]    new=$(leader_of "$P_NEW") (READY before taking traffic)"

echo "[upgrade] 3) draining the OLD node; the NEW node's API must stay up the whole time"
kill_port "$P_OLD"
T0=$(date +%s); SERVED=1
for i in $(seq 1 20); do
  api_up "$P_NEW" || { SERVED=0; echo "[upgrade]    !! the new node's API went down during the switch"; }
  [ "$(leader_of "$P_NEW")" = "leader" ] && { echo "[upgrade]    the new node took leadership in ~$(( $(date +%s) - T0 ))s"; break; }
  sleep 1
done

[ "$(leader_of "$P_NEW")" = "leader" ] || echo "FAILED: the new node did not take leadership"
[ "$SERVED" = "1" ] && echo "[upgrade] OK — the API stayed available throughout the rolling upgrade" || echo "FAILED: the API was unavailable at some point"

echo "[upgrade] cleaning up…"; kill_port "$P_OLD"; kill_port "$P_NEW"; rm -rf "$WS"
echo "[upgrade] done."
