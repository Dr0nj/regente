#!/usr/bin/env sh
# Chaos/HA — valida o failover de liderança (R3/G1) contra um Postgres real.
# Sobe 2 nós no MESMO banco, confirma 1 líder único, MATA o líder e mede o failover.
#
# Uso:
#   REGENTE_BIN=./regente-server REGENTE_PG_DSN="postgres://u:p@host:5432/db?sslmode=disable" \
#     ./chaos-ha.sh
#
# Requer: o binário do server + um Postgres alcançável (não usa Docker diretamente).
set -eu

BIN="${REGENTE_BIN:-./regente-server}"
DSN="${REGENTE_PG_DSN:?defina REGENTE_PG_DSN (postgres://...)}"
PA="${PORT_A:-9080}"
PB="${PORT_B:-9081}"
WS="$(mktemp -d)"
mkdir -p "$WS/a/definitions" "$WS/b/definitions"

leader_of() { # $1 = porta → imprime "leader"/"follower"
  curl -s "http://127.0.0.1:$1/readyz" 2>/dev/null | grep -o '"detail":"[a-z]*"' | head -1 | cut -d'"' -f4
}
wait_ready() { for i in $(seq 1 20); do [ "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$1/readyz" 2>/dev/null)" = "200" ] && return 0; sleep 1; done; return 1; }
kill_port() { p=$(netstat -ano 2>/dev/null | grep LISTENING | grep ":$1 " | awk '{print $NF}' | head -1); [ -n "${p:-}" ] && { taskkill //PID "$p" //F >/dev/null 2>&1 || kill -9 "$p" 2>/dev/null; }; }

echo "[chaos] subindo nó A (:$PA) e B (:$PB) no mesmo Postgres…"
nohup "$BIN" -addr=":$PA" -db-driver=postgres -db="$DSN" -workspace="$WS/a" -git-source="" -github-repo="" -node-id=chaosA >"$WS/a.log" 2>&1 &
wait_ready "$PA" || { echo "nó A não subiu"; exit 1; }
nohup "$BIN" -addr=":$PB" -db-driver=postgres -db="$DSN" -workspace="$WS/b" -git-source="" -github-repo="" -node-id=chaosB >"$WS/b.log" 2>&1 &
wait_ready "$PB" || { echo "nó B não subiu"; exit 1; }

LA="$(leader_of "$PA")"; LB="$(leader_of "$PB")"
echo "[chaos] A=$LA  B=$LB"
[ "$LA" = "leader" ] && [ "$LB" = "follower" ] || [ "$LA" = "follower" ] && [ "$LB" = "leader" ] || { echo "FALHA: não há exatamente 1 líder"; }

# Mata o líder e mede o failover no follower.
if [ "$LA" = "leader" ]; then LEADER_PORT="$PA"; SURV="$PB"; else LEADER_PORT="$PB"; SURV="$PA"; fi
echo "[chaos] matando o líder (:$LEADER_PORT)…"
kill_port "$LEADER_PORT"
T0=$(date +%s)
for i in $(seq 1 20); do
  [ "$(leader_of "$SURV")" = "leader" ] && { echo "[chaos] OK — :$SURV assumiu a liderança em ~$(( $(date +%s) - T0 ))s"; break; }
  sleep 1
done
[ "$(leader_of "$SURV")" = "leader" ] || echo "FALHA: o follower não assumiu"

echo "[chaos] limpando…"
kill_port "$PA"; kill_port "$PB"; rm -rf "$WS"
echo "[chaos] fim."
