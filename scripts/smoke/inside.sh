#!/usr/bin/env bash
# inside.sh — roda DENTRO do container do smoke test (systemd real).
#
# Não é um teste de unidade: é a instalação como um operador faz, contra o
# artefato que está prestes a ser publicado. Cada asserção aqui nasceu de um
# defeito real encontrado instalando à mão (ver §V-AUDIT no docs/roadmap.md).
#
#   stage1 — instala do bundle, quebra a config de propósito, conserta, sobe
#            agente e executa um job de ponta a ponta.
#   stage2 — roda DEPOIS do reboot do container: prova que o serviço volta
#            sozinho e que o estado sobreviveu.
set -uo pipefail

STAGE="${1:-stage1}"
PORT=8080
BASE="http://127.0.0.1:$PORT"
WS=/var/lib/regente/workspace
ENV_FILE=/etc/regente/server.env

fails=0
ok()   { echo "   [ok]   $*"; }
bad()  { echo "   [FAIL] $*"; fails=$((fails + 1)); }
head1() { echo; echo "== $*"; }

tok() { grep '^REGENTE_TOKEN=' "$ENV_FILE" | tail -1 | cut -d= -f2-; }
api() { curl -fsS --max-time 10 -H "Authorization: Bearer $(tok)" "$@"; }
code() { curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$1"; }
jfield() { python3 -c "import sys,json;print(json.load(sys.stdin).get('$1',''))"; }

# wait_for <segundos> <comando> — repete até o comando passar (evita sleep fixo,
# que é a fonte clássica de flake em CI).
wait_for() {
  local secs="$1"; shift
  local i=0
  while [ "$i" -lt "$secs" ]; do
    if eval "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1; i=$((i + 1))
  done
  return 1
}

active()    { [ "$(systemctl is-active regente-server)" = active ]; }
nrestarts() { systemctl show regente-server -p NRestarts --value; }
gitfield()  { api "$BASE/api/git/status" | jfield "$1"; }

set_source() { # set_source <url|path>  — reescreve/insere REGENTE_GIT_SOURCE
  sed -i -E 's|^[[:space:]]*#?[[:space:]]*REGENTE_GIT_SOURCE=.*|REGENTE_GIT_SOURCE='"$1"'|' "$ENV_FILE"
  grep -q '^REGENTE_GIT_SOURCE=' "$ENV_FILE" || echo "REGENTE_GIT_SOURCE=$1" >> "$ENV_FILE"
}

if [ "$STAGE" = stage2 ]; then
  head1 "Depois do reboot da máquina"
  wait_for 60 active || bad "o serviço não voltou sozinho depois do reboot"
  active && ok "serviço ativo sem ninguém mandar (systemd enable)"
  [ "$(code "$BASE/health")" = 200 ] && ok "health 200" || bad "health não respondeu 200 após o reboot"
  [ -f /var/lib/regente/regente.db ] && ok "banco preservado" || bad "o banco sumiu no reboot"
  [ -f /var/lib/regente/app/index.html ] && ok "UI preservada" || bad "a UI sumiu no reboot"
  wait_for 30 '[ -n "$(gitfield sha)" ]' || bad "workspace não voltou sincronizado"
  [ -n "$(gitfield sha)" ] && ok "workspace ainda clonado (sha $(gitfield sha | cut -c1-7))"
  [ -z "$(gitfield error)" ] && ok "GitOps sem erro" || bad "GitOps com erro após o reboot: $(gitfield error)"
  echo
  [ "$fails" = 0 ] && { echo "SMOKE stage2 OK"; exit 0; } || { echo "SMOKE stage2: $fails falha(s)"; exit 1; }
fi

# ────────────────────────────────────────────────────────────────── stage 1 ───
head1 "1) Instalação a partir do bundle (o mesmo que o install.sh baixa)"
# --warning=no-timestamp: o relógio do host pode estar à frente do container e o
# tar reclama de cada arquivo — ruído puro, não afeta a extração.
mkdir -p /opt/bundle && tar --warning=no-timestamp -xzf /root/bundle.tar.gz -C /opt/bundle
BDIR="$(find /opt/bundle -maxdepth 1 -mindepth 1 -type d | head -1)"
[ -x "$BDIR/regente-server" ] || { echo "bundle inválido: sem binário"; exit 1; }
bash "$BDIR/deploy/install-linux.sh" > /tmp/install.log 2>&1 || { echo "installer falhou:"; cat /tmp/install.log; exit 1; }

wait_for 30 active || bad "o serviço não ficou ativo"
active && ok "serviço ativo"
[ "$(systemctl is-enabled regente-server)" = enabled ] && ok "habilitado no boot" || bad "não ficou enabled"
wait_for 30 '[ "$(code "'"$BASE"'/health")" = 200 ]' || bad "health nunca respondeu 200"
[ "$(code "$BASE/health")" = 200 ] && ok "health 200"
curl -fsS "$BASE/" 2>/dev/null | grep -qi '<!doctype html' && ok "UI servida na mesma porta" || bad "a UI não está sendo servida"
[ -x /usr/local/bin/regente-configure ] && ok "regente-configure instalado" || bad "regente-configure não foi instalado"
grep -q 'firewall' /tmp/install.log && ok "instalador lembra do firewall" || bad "instalador não fala do firewall"
grep -q 'Health:' /tmp/install.log && ok "instalador imprime health check" || bad "instalador não faz health check"
# Versão: prova a cadeia toda (ldflags -X main.version → binário → API → rodapé).
# Só vale quando o bundle foi buildado por --build, que injeta "smoke-test"; um
# bundle da CI traz a tag da release, então aceitamos qualquer valor != "dev".
srv_ver="$(api "$BASE/api/version" | jfield version)"
case "$srv_ver" in
  ""|dev) bad "GET /api/version não trouxe versão injetada (veio '${srv_ver:-vazio}')" ;;
  *)      ok "versão do build exposta na API: $srv_ver" ;;
esac
[ "$(/usr/local/bin/regente-server -version 2>/dev/null)" = "$srv_ver" ] \
  && ok "regente-server -version bate com a API" \
  || bad "o -version do binário não bate com o que a API reporta"

head1 "2) Config quebrada (repo inexistente) NÃO pode derrubar o serviço"
set_source "https://github.com/regente-smoke/nao-existe-$RANDOM.git"
systemctl restart regente-server
sleep 8
active && ok "serviço continua ativo (sem crash-loop)" || bad "o serviço caiu com repo inválido"
[ "$(nrestarts)" = 0 ] && ok "NRestarts=0" || bad "systemd reiniciou o processo ($(nrestarts)x) = crash-loop"
[ "$(code "$BASE/health")" = 200 ] && ok "UI/API seguem acessíveis" || bad "health caiu com repo inválido"
[ -n "$(gitfield error)" ] && ok "motivo exposto na API: $(gitfield error | cut -c1-60)…" || bad "a API não conta por que o GitOps não subiu"
[ ! -d "$WS/.git" ] && ok "não sobrou .git parcial (recuperação continua possível)" || bad "clone falho deixou .git para trás — a instalação trava para sempre"

head1 "3) Repositório VAZIO: o server escreve o conteúdo inicial"
git init --bare -q /srv/vazio.git
set_source /srv/vazio.git
systemctl restart regente-server
wait_for 45 '[ -n "$(gitfield sha)" ]' || bad "não sincronizou com o repositório vazio"
[ -n "$(gitfield sha)" ] && ok "sincronizado (sha $(gitfield sha | cut -c1-7))"
[ -z "$(gitfield error)" ] && ok "sem erro" || bad "erro após o bootstrap: $(gitfield error)"
git --git-dir=/srv/vazio.git ls-tree -r --name-only main 2>/dev/null | grep -q 'definitions/.gitkeep' \
  && ok "conteúdo inicial publicado no repositório" || bad "o bootstrap não publicou nada no remote"

head1 "4) Repositório JÁ USADO (migração): as definitions voltam e viram ordens"
git init --bare -q /srv/usado.git
rm -rf /tmp/seed && git init -q -b main /tmp/seed && git -C /tmp/seed remote add origin /srv/usado.git
mkdir -p /tmp/seed/definitions/ops
cat > /tmp/seed/definitions/ops/smoke-job.yaml <<'YAML'
id: smoke-job
label: Smoke job
team: ops
jobType: COMMAND
schedule:
  enabled: true
  frequency: daily
timeout: 60
params:
  command: echo "smoke-ok"
YAML
printf 'name: ops\nlayout:\n  columns: 4\n' > /tmp/seed/definitions/ops/.regente-folder.yaml
git -C /tmp/seed -c user.email=s@s -c user.name=s add -A >/dev/null
git -C /tmp/seed -c user.email=s@s -c user.name=s commit -qm seed >/dev/null
git -C /tmp/seed push -q origin main
# Máquina nova: banco e workspace zerados, só o repositório sobrevive.
systemctl stop regente-server
rm -f /var/lib/regente/regente.db*; rm -rf "$WS"
set_source /srv/usado.git
systemctl start regente-server
wait_for 60 'api "'"$BASE"'/api/definitions" | grep -q smoke-job' || bad "as definitions do repositório não carregaram"
api "$BASE/api/definitions" | grep -q smoke-job && ok "definition veio do repositório"
api "$BASE/api/folders" | grep -q '"columns":4' && ok "layout do .regente-folder.yaml veio junto" || bad "o override de grade do folder não foi lido"
wait_for 60 'api "'"$BASE"'/api/instances" | grep -q smoke-job' || bad "a daily não materializou a ordem do dia"
api "$BASE/api/instances" | grep -q smoke-job && ok "daily materializou a ordem"

head1 "5) Agente instalado como serviço e job executando de verdade"
# URL propositalmente no formato da UI (http://host:porta): o instalador tem de
# convertê-la no endpoint do agente sozinho.
SERVER="http://127.0.0.1:$PORT" TOKEN="$(tok)" ID=smoke-agent CAPS=COMMAND,SCRIPT,HTTP \
  bash /root/agent-deploy/install-linux.sh > /tmp/agent.log 2>&1 || { echo "installer do agente falhou:"; cat /tmp/agent.log; fails=$((fails+1)); }
grep -q 'ws://127.0.0.1:8080/ws/agent' /etc/systemd/system/regente-agent.service \
  && ok "URL da UI normalizada para o endpoint do agente" || bad "a URL do agente não foi normalizada"
# A checagem é na ExecStart, não no arquivo inteiro: a unit MENCIONA a palavra
# "token" num comentário explicando justamente que ele não vai por ali.
if grep -E '^ExecStart=.*-token' /etc/systemd/system/regente-agent.service >/dev/null; then
  bad "o token do agente está na linha de comando (visível no ps)"
else
  ok "token do agente fora da linha de comando"
fi
[ "$(stat -c %a /etc/regente/agent.env 2>/dev/null)" = 600 ] && ok "agent.env em 0600" \
  || bad "o arquivo com o token do agente não está em 0600"
wait_for 45 'api "'"$BASE"'/api/agents" | grep -q smoke-agent' || bad "o agente não apareceu online"
api "$BASE/api/agents" | grep -q smoke-agent && ok "agente online na frota"

api -X POST "$BASE/api/definitions/smoke-job/force" >/dev/null 2>&1 || bad "Order Force falhou"
wait_for 90 'api "'"$BASE"'/api/instances" | grep -q "\"status\":\"OK\""' || bad "o job não terminou OK"
if api "$BASE/api/instances" | grep -q '"status":"OK"'; then
  ok "job executou pelo agente e terminou OK"
  iid="$(api "$BASE/api/instances" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d[0]['id'] if d else '')")"
  if api "$BASE/api/instances/$iid/output" | grep -q 'smoke-ok'; then
    ok "output do processo chegou na API"
  else
    bad "o output do job não chegou"
  fi
fi


# ---------------------------------------------------------------------------
# Borda: nginx na frente, como manda o deploy/vps/README.md.
#
# Esta seção existe porque a PRIMEIRA instalação real em VPS quebrou toda ela —
# e nada disso estava sob o smoke: o `deploy/vps/` simplesmente não existia na
# máquina (o one-liner baixa o bundle num mktemp -d e apaga na saída), então o
# passo 2 do próprio README mandava copiar um arquivo inexistente. TLS e DNS
# ficam de fora por dependerem de rede externa; o que dá pra provar aqui é o
# caminho inteiro até o proxy responder.
head1 "Borda (nginx reverse proxy)"

VPS_DIR=/var/lib/regente/deploy/vps
if [ -f "$VPS_DIR/nginx-regente.conf" ] && [ -f "$VPS_DIR/enable-tls.sh" ]; then
  ok "deploy/vps instalado em $VPS_DIR (o README manda copiar daqui)"
else
  bad "deploy/vps NÃO foi instalado — o passo 2 do deploy/vps/README.md aponta para um arquivo que não existe"
fi

if command -v nginx >/dev/null 2>&1 && [ -f "$VPS_DIR/nginx-regente.conf" ]; then
  SMOKE_DOMAIN=smoke.regente.test
  cp "$VPS_DIR/nginx-regente.conf" /etc/nginx/conf.d/regente.conf
  sed -i "s/REGENTE_DOMAIN/$SMOKE_DOMAIN/" /etc/nginx/conf.d/regente.conf
  if nginx -t >/tmp/nginx-t.log 2>&1; then
    ok "nginx -t aceita o nginx-regente.conf"
  else
    bad "nginx -t recusou o nginx-regente.conf:"; cat /tmp/nginx-t.log
  fi
  # `systemctl restart` (não reload): o serviço pode nem ter subido no boot do
  # container, e reload de um nginx parado falha sem dizer o porquê.
  systemctl restart nginx >/dev/null 2>&1 || true
  if wait_for 20 'systemctl is-active nginx | grep -q "^active$"'; then
    ok "nginx ativo"
    # Host forjado: sem DNS, é o cabeçalho que faz o server_name casar. Prova o
    # circuito completo browser -> :80 -> proxy -> regente-server no loopback.
    edge() { curl -s -o /dev/null -w '%{http_code}' --max-time 10 -H "Host: $SMOKE_DOMAIN" "http://127.0.0.1$1"; }
    [ "$(edge /health)" = 200 ] && ok "health atravessa o proxy (200)" \
      || bad "health pelo proxy não voltou 200 (veio $(edge /health))"
    [ "$(edge /)" = 200 ] && ok "UI servida pelo proxy (200 em /)" \
      || bad "a UI não veio pelo proxy (veio $(edge /))"
    # A borda tem de repassar o Authorization: sem isso a API responde 401 e o
    # sintoma aparece só depois, na UI logada.
    aut="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 -H "Host: $SMOKE_DOMAIN" -H "Authorization: Bearer $(tok)" "http://127.0.0.1/api/definitions")"
    [ "$aut" = 200 ] && ok "API autenticada pelo proxy (200)" || bad "API pelo proxy voltou $aut"
    curl -s -D- -o /dev/null --max-time 10 -H "Host: $SMOKE_DOMAIN" "http://127.0.0.1/" \
      | grep -qi 'X-Content-Type-Options: nosniff' \
      && ok "headers de hardening da borda presentes" || bad "a borda não devolveu os headers de hardening"
  else
    bad "nginx não ficou ativo: $(systemctl is-active nginx) — $(journalctl -u nginx -n 5 --no-pager 2>/dev/null | tr '\n' ' ')"
  fi
else
  echo "   [skip] nginx não está na imagem — seção da borda não executada"
fi


# ---------------------------------------------------------------------------
# Upgrade: reinstalar POR CIMA de uma instalação viva.
#
# É o caminho de atualização que o README manda usar, e nunca era testado — o
# smoke sempre instalava numa máquina limpa. O defeito que isso escondia:
# `systemctl enable --now` só INICIA uma unit parada; numa unit já ativa o start
# é no-op, então o binário novo ia pro disco e o processo ANTIGO continuava
# rodando. Reinstalar parecia funcionar e não mudava nada.
head1 "Upgrade in place (reinstalar por cima)"

pid_of() { systemctl show regente-server -p MainPID --value 2>/dev/null; }
pid_before="$(pid_of)"
tok_before="$(tok)"
inst_before="$(api "$BASE/api/instances" | python3 -c "import sys,json;print(len(json.load(sys.stdin)))" 2>/dev/null || echo -1)"

bash "$BDIR/deploy/install-linux.sh" > /tmp/upgrade.log 2>&1 || { echo "reinstalação falhou:"; cat /tmp/upgrade.log; fails=$((fails+1)); }

grep -q 'UPGRADED in place' /tmp/upgrade.log \
  && ok "instalador reconheceu que era upgrade" || bad "o instalador tratou a reinstalação como instalação nova"
pid_after="$(pid_of)"
if [ -n "$pid_before" ] && [ "$pid_before" != 0 ] && [ "$pid_after" != "$pid_before" ]; then
  ok "processo trocado ($pid_before -> $pid_after): a versão nova está NO AR"
else
  bad "o processo NÃO reiniciou (PID $pid_before -> $pid_after) — o binário antigo continua rodando"
fi
grep -q 'Process replaced' /tmp/upgrade.log && ok "instalador PROVA a troca no output" \
  || bad "o instalador não prova que o processo foi substituído"

wait_for 30 '[ "$(code "'"$BASE"'/health")" = 200 ]' || bad "health não voltou depois do upgrade"
[ "$(code "$BASE/health")" = 200 ] && ok "health 200 depois do upgrade"
[ "$(tok)" = "$tok_before" ] && ok "server.env preservado (token intacto)" || bad "a reinstalação sobrescreveu o server.env"
inst_after="$(api "$BASE/api/instances" | python3 -c "import sys,json;print(len(json.load(sys.stdin)))" 2>/dev/null || echo -2)"
[ "$inst_after" = "$inst_before" ] && ok "banco preservado ($inst_after instance(s))" \
  || bad "o histórico mudou no upgrade (antes=$inst_before depois=$inst_after)"
[ -f /var/lib/regente/deploy/vps/nginx-regente.conf ] && ok "deploy/vps continua instalado" \
  || bad "a reinstalação perdeu o deploy/vps"

echo
if [ "$fails" = 0 ]; then echo "SMOKE stage1 OK"; exit 0; else echo "SMOKE stage1: $fails falha(s)"; exit 1; fi
