#!/usr/bin/env bash
# live-check.sh — diagnóstico de uma instalação REAL do Regente, rodado NO VPS.
#
# Por que existe: o `smoke-install.sh` prova a instalação num container efêmero e
# para aí. Uma instância 24/7 falha por coisas que container nenhum vive — o
# certificado que vence em 60 dias, o disco que enche em três semanas, o tick que
# parou às 4 da manhã, o backup que ninguém nunca tentou restaurar. Este script
# roda contra a máquina viva e responde: "posso dormir tranquilo hoje?".
#
# É SÓ LEITURA. Não reinicia serviço, não escreve no banco, não toca na config.
# O backup vai pra um diretório temporário e é apagado no fim.
#
# Uso:   sudo bash live-check.sh [dominio]           (checagens rápidas)
#        sudo bash live-check.sh [dominio] --deep    (inclui certbot renew --dry-run)
#
# Sem argumento, o domínio é lido do conf do nginx. Saída != 0 = tem coisa pra ver.

set -uo pipefail

DOMAIN="${1:-}"
[ "${DOMAIN:-}" = "--deep" ] && { DOMAIN=""; DEEP=1; } || DEEP=0
[ "${2:-}" = "--deep" ] && DEEP=1

ENV_FILE=/etc/regente/server.env
DATA_DIR=/var/lib/regente
UNIT=regente-server
PORT="${REGENTE_PORT:-8080}"
BASE="http://127.0.0.1:$PORT"

fails=0; warns=0
ok()   { echo "   [ok]   $*"; }
bad()  { echo "   [FAIL] $*"; fails=$((fails + 1)); }
warn() { echo "   [warn] $*"; warns=$((warns + 1)); }
head1() { echo; echo "== $*"; }

tok()  { grep '^REGENTE_TOKEN=' "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2-; }
api()  { curl -fsS --max-time 10 -H "Authorization: Bearer $(tok)" "$@" 2>/dev/null; }
code() { curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$@"; }
jfield() { python3 -c "import sys,json;print(json.load(sys.stdin).get('$1',''))" 2>/dev/null; }

[ -f "$ENV_FILE" ] || { echo "não achei $ENV_FILE — este script roda NO servidor instalado."; exit 2; }

# ---------------------------------------------------------------------------
head1 "Serviço"

state="$(systemctl is-active $UNIT 2>/dev/null)"
[ "$state" = active ] && ok "unit ativa" || bad "unit em '$state'"
systemctl is-enabled $UNIT >/dev/null 2>&1 \
  && ok "habilitada no boot" \
  || bad "NÃO habilitada no boot — um reboot derruba a instância pra sempre"

# NRestarts que sobe sozinho é crash-loop mascarado pelo Restart=always: o serviço
# parece de pé porque o systemd fica ressuscitando, e o sintoma real é intermitente.
nr="$(systemctl show $UNIT -p NRestarts --value 2>/dev/null)"
if [ "${nr:-0}" -eq 0 ]; then ok "nunca reiniciou desde o boot"
elif [ "${nr:-0}" -lt 3 ]; then warn "reiniciou $nr vez(es) — olhe o journal"
else bad "reiniciou $nr vezes: isso é crash-loop escondido pelo Restart=always"; fi

since="$(systemctl show $UNIT -p ActiveEnterTimestamp --value 2>/dev/null)"
[ -n "$since" ] && echo "          no ar desde: $since"
command -v regente-server >/dev/null && echo "          versão: $(regente-server -version 2>/dev/null | head -1)"

# ---------------------------------------------------------------------------
head1 "Saúde local (sem passar pela borda)"

for p in /health /livez /readyz; do
  c="$(code "$BASE$p")"
  [ "$c" = 200 ] && ok "$p 200" || bad "$p devolveu $c"
done

# ---------------------------------------------------------------------------
head1 "Scheduler e dados"

# O tick parado é a falha mais silenciosa que existe: a API responde, a UI abre,
# e simplesmente nada mais é despachado. Comparar o relógio do server com a
# última daily é a leitura mais barata disso.
ds="$(api "$BASE/api/daily/status")"
if [ -n "$ds" ]; then
  order="$(printf '%s' "$ds" | jfield orderDate)"
  last="$(printf '%s' "$ds" | jfield lastRunDate)"
  now="$(printf '%s' "$ds" | jfield serverNow)"
  echo "          order_date=$order · última daily=$last · relógio do server=$now"
  [ -n "$last" ] && ok "daily já rodou nesta instalação" || bad "nenhuma daily registrada"
  [ "$last" = "$order" ] && ok "a daily do dia corrente está carimbada" \
    || warn "última daily ($last) != order_date ($order) — normal antes do horário, suspeito depois"
else
  bad "não consegui ler /api/daily/status (token errado no $ENV_FILE?)"
fi

m="$(curl -fsS --max-time 10 "$BASE/metrics" 2>/dev/null)"
if [ -n "$m" ]; then
  ag="$(printf '%s' "$m" | awk '/^regente_agents_online /{print $2}')"
  [ "${ag:-0}" -gt 0 ] && ok "$ag agente(s) online" \
    || warn "nenhum agente online — jobs que não sejam HTTP ficam em WAIT AGENT"
  printf '%s' "$m" | awk '/^regente_instances\{/{gsub(/.*status="|".*/,"",$0)}' >/dev/null
  echo "          instâncias de hoje: $(printf '%s' "$m" | grep -c '^regente_instances{') status distinto(s)"
fi

gs="$(api "$BASE/api/git/status")"
if [ -n "$gs" ]; then
  err="$(printf '%s' "$gs" | jfield error)"
  sha="$(printf '%s' "$gs" | jfield sha)"
  [ -z "$err" ] && ok "GitOps sem erro (sha ${sha:0:7})" || bad "GitOps com erro: $err"
fi

# ---------------------------------------------------------------------------
head1 "Borda (nginx + TLS)"

if [ -z "$DOMAIN" ] && [ -f /etc/nginx/conf.d/regente.conf ]; then
  DOMAIN="$(awk '/server_name/{print $2}' /etc/nginx/conf.d/regente.conf | head -1 | tr -d ';')"
fi

if [ -z "$DOMAIN" ]; then
  warn "domínio não informado e não achei no nginx — pulando a borda"
else
  echo "          domínio: $DOMAIN"
  c="$(code "https://$DOMAIN/health")"
  [ "$c" = 200 ] && ok "health pela internet (HTTPS 200)" || bad "https://$DOMAIN/health devolveu $c"
  c="$(code "https://$DOMAIN/")"
  [ "$c" = 200 ] && ok "UI servida por HTTPS" || bad "a UI por HTTPS devolveu $c"

  # 80 tem que redirecionar, não servir: senão existe uma porta em claro servindo
  # a mesma aplicação e o TLS vira decoração.
  c="$(code "http://$DOMAIN/")"
  case "$c" in
    30*) ok "porta 80 redireciona pra HTTPS ($c)" ;;
    200) bad "porta 80 SERVE a aplicação em claro (200) em vez de redirecionar" ;;
    *)   warn "porta 80 devolveu $c" ;;
  esac

  # O upgrade do WebSocket é o que faz a UI atualizar sozinha. Se ele quebrar,
  # todo o resto continua 200 e o operador vê uma tela que "trava".
  key="$(head -c 16 /dev/urandom | base64)"
  : > /tmp/live-ws.txt
  curl -s -i -N --max-time 6 -o /tmp/live-ws.txt \
    -H "Authorization: Bearer $(tok)" \
    -H "Connection: Upgrade" -H "Upgrade: websocket" \
    -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: $key" \
    "https://$DOMAIN/ws/web" >/dev/null 2>&1 || true
  line="$(head -1 /tmp/live-ws.txt | tr -d '\r')"
  case "$line" in
    *101*) ok "WebSocket faz upgrade pelo TLS (101) — a UI atualiza ao vivo" ;;
    *)     bad "WebSocket NÃO subiu pela borda: '$line' — a UI carrega e congela" ;;
  esac
  rm -f /tmp/live-ws.txt

  if command -v openssl >/dev/null 2>&1; then
    endd="$(echo | openssl s_client -servername "$DOMAIN" -connect "$DOMAIN:443" 2>/dev/null \
            | openssl x509 -noout -enddate 2>/dev/null | cut -d= -f2)"
    if [ -n "$endd" ]; then
      left=$(( ( $(date -d "$endd" +%s) - $(date +%s) ) / 86400 ))
      if   [ "$left" -gt 21 ]; then ok "certificado vence em $left dias"
      elif [ "$left" -gt 7 ];  then warn "certificado vence em $left dias — a renovação já devia ter rodado"
      else bad "certificado vence em $left dias"; fi
    fi
  fi
fi

if [ "$DEEP" = 1 ] && command -v certbot >/dev/null 2>&1; then
  # A renovação é o único passo do TLS que ninguém exercita — e ela falha calada,
  # 60 dias depois de tudo estar funcionando.
  if certbot renew --dry-run >/tmp/certbot-dry.log 2>&1; then
    ok "certbot renew --dry-run passou"
  else
    bad "certbot renew --dry-run FALHOU — o cert vai vencer sem avisar:"; tail -5 /tmp/certbot-dry.log
  fi
fi

# ---------------------------------------------------------------------------
head1 "Disco, banco e backup"

free_kb="$(df -Pk "$DATA_DIR" | awk 'NR==2{print $4}')"
use_pct="$(df -P "$DATA_DIR" | awk 'NR==2{gsub(/%/,"",$5); print $5}')"
echo "          $DATA_DIR: $((free_kb / 1024)) MiB livres (uso ${use_pct}%)"
if   [ "$use_pct" -lt 80 ]; then ok "disco folgado"
elif [ "$use_pct" -lt 90 ]; then warn "disco em ${use_pct}% — olhe retenção/archives"
else bad "disco em ${use_pct}%: o SQLite para de escrever antes de encher de vez"; fi

DB="$(grep '^REGENTE_DB=' "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2-)"
DB="${DB:-$DATA_DIR/regente.db}"
[ -f "$DB" ] && echo "          banco: $DB ($(du -h "$DB" | cut -f1))"

# Backup que ninguém tentou restaurar não é backup. Aqui prova-se as duas metades
# baratas: o -backup completa, e o arquivo resultante abre íntegro.
if command -v regente-server >/dev/null 2>&1; then
  TMPD="$(mktemp -d)"
  if regente-server -backup "$TMPD/bkp.db" >/tmp/bkp.log 2>&1 && [ -s "$TMPD/bkp.db" ]; then
    ok "backup online gerado ($(du -h "$TMPD/bkp.db" | cut -f1))"
    if command -v sqlite3 >/dev/null 2>&1; then
      r="$(sqlite3 "$TMPD/bkp.db" 'PRAGMA integrity_check;' 2>&1 | head -1)"
      [ "$r" = ok ] && ok "backup passa no integrity_check" || bad "backup CORROMPIDO: $r"
      n="$(sqlite3 "$TMPD/bkp.db" 'SELECT count(*) FROM instances;' 2>/dev/null)"
      [ -n "$n" ] && ok "backup tem dado de verdade ($n instances)" || warn "não consegui contar instances no backup"
    else
      warn "sqlite3 não instalado — integridade do backup não verificada (apt install sqlite3)"
    fi
  else
    bad "o backup online falhou:"; tail -3 /tmp/bkp.log
  fi
  rm -rf "$TMPD"
fi

# ---------------------------------------------------------------------------
head1 "Journal (últimas 24h)"

errs="$(journalctl -u $UNIT --since '24 hours ago' -p err --no-pager 2>/dev/null | grep -vc '^-- ' || true)"
if [ "${errs:-0}" -eq 0 ]; then ok "nenhum erro no journal"
else
  warn "$errs linha(s) de erro nas últimas 24h — as 5 últimas:"
  journalctl -u $UNIT --since '24 hours ago' -p err --no-pager 2>/dev/null | tail -5 | sed 's/^/          /'
fi

# ---------------------------------------------------------------------------
echo
if [ "$fails" -eq 0 ] && [ "$warns" -eq 0 ]; then
  echo "✅ live-check: tudo verde."
elif [ "$fails" -eq 0 ]; then
  echo "🟡 live-check: $warns aviso(s), nenhuma falha."
else
  echo "❌ live-check: $fails falha(s) e $warns aviso(s)."
fi
exit $(( fails > 0 ? 1 : 0 ))
