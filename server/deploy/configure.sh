#!/usr/bin/env bash
# V3 — configuração GUIADA do regente-server: pergunta o essencial e escreve o
# /etc/regente/server.env (token forte, GitHub PAT/repo, domínio). Rode DEPOIS do install:
#
#   sudo regente-configure        (instalado pelo install-linux.sh)
#   # ou, do checkout/bundle:  sudo bash server/deploy/configure.sh
#
# Precisa de TERMINAL interativo — não funciona via `curl | sudo bash` (stdin ocupado).
set -euo pipefail

ENV_FILE=/etc/regente/server.env
[ "$(id -u)" = 0 ]  || { echo "run as root (sudo)"; exit 1; }
[ -t 0 ]            || { echo "an interactive terminal is required — run 'sudo regente-configure' directly (not through a pipe)."; exit 1; }
[ -f "$ENV_FILE" ]  || { echo "$ENV_FILE does not exist — run the installer first (install-linux.sh)."; exit 1; }

gen_token() {
  if command -v openssl >/dev/null 2>&1; then openssl rand -hex 24
  else tr -dc 'a-f0-9' </dev/urandom | head -c 48; echo; fi
}
current() { grep -E "^[[:space:]]*${1}=" "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2- || true; }

# Bracketed paste: com o terminal nesse modo, um `read` recebe
# \e[200~<texto>\e[201~ — e com -s (silencioso) NADA disso aparece na tela. O
# valor era gravado com o lixo junto, o systemd DESCARTAVA a linha inteira do
# EnvironmentFile por causa do caractere de controle, e o que chegava ao operador
# era "git clone: authentication required: Repository not found." — três saltos
# longe da causa, e com cara de permissão do GitHub. Desligamos o modo e AINDA
# ASSIM limpamos o que chegar (o terminal pode reativá-lo entre prompts).
printf '\033[?2004l' 2>/dev/null || true
clean() {
  printf '%s' "${1-}" \
    | sed -e 's/\x1b\[20[01]~//g' -e 's/\[20[01]~//g' \
    | tr -d '\000-\037\177' \
    | sed -e 's/^[[:space:]]\{1,\}//' -e 's/[[:space:]]\{1,\}$//'
}

set_env() {  # set_env KEY VALUE  (upsert idempotente; descomenta se preciso)
  local key="$1" val="$2" esc
  # Rede de segurança do `clean`: nunca gravar caractere de controle no
  # EnvironmentFile. Falhar alto é melhor que gravar uma linha que o systemd
  # descarta em silêncio.
  case "$val" in
    *[[:cntrl:]]*)
      echo "   ! refusing to write $key: the value has control characters (a paste artefact?)." >&2
      return 1 ;;
  esac
  esc=$(printf '%s' "$val" | sed -e 's/[&|]/\\&/g')
  if grep -qE "^[[:space:]]*#?[[:space:]]*${key}=" "$ENV_FILE"; then
    sed -i -E "s|^[[:space:]]*#?[[:space:]]*${key}=.*|${key}=${esc}|" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$val" >> "$ENV_FILE"
  fi
}

echo "== Regente guided setup — Enter keeps the suggested/current value =="
echo ""

# 1) Token de API — admin-equivalente (bypassa login). NUNCA deixe dev-token/change-me.
cur_tok="$(current REGENTE_TOKEN)"
sug_tok="$cur_tok"
case "$sug_tok" in ''|dev-token|change-me) sug_tok="$(gen_token)";; esac
read -rp "REGENTE_TOKEN (strong API token) [$sug_tok]: " tok
tok="$(clean "$tok")"
tok="${tok:-$sug_tok}"
set_env REGENTE_TOKEN "$tok"

# 2) GitHub — repo do workspace (GitOps) + PAT via secrets provider (fora da DB em claro).
# SEM default de repositório: o workspace é SEU e é diferente em cada instalação.
# Sugerir um repo concreto aqui já fez toda instalação nova apontar para o mesmo lab.
echo ""
echo "-- Workspace (GitOps): the repository that stores YOUR job definitions."
echo "   Create an EMPTY repository on GitHub (private is fine) and paste it below as"
echo "   owner/name — the server writes the initial content into it on the first start."
echo "   Leave it EMPTY to run offline (definitions live only on this machine's disk)."
cur_repo="$(current REGENTE_GIT_REPO)"
while :; do
  read -rp "Your workspace repo (owner/name)${cur_repo:+ [$cur_repo]}: " repo
  repo="$(clean "$repo")"
  repo="${repo:-$cur_repo}"
  case "$repo" in
    "") break ;;                                   # vazio = offline, aceito
    */*/*|http*|git@*)
      echo "   ! use the short form owner/name (not a URL). Example: acme/regente-workspace" ;;
    */*) break ;;
    *)  echo "   ! expected owner/name. Example: acme/regente-workspace" ;;
  esac
done
if [ -n "$repo" ]; then
  set_env REGENTE_GIT_REPO "$repo"
  set_env REGENTE_GIT_SOURCE "https://github.com/${repo}.git"
  cur_br="$(current REGENTE_GIT_BRANCH)"
  read -rp "Branch [${cur_br:-main}]: " br
  br="$(clean "$br")"
  set_env REGENTE_GIT_BRANCH "${br:-${cur_br:-main}}"
else
  # Offline explícito: comenta as chaves para o server não tentar clonar nada.
  sed -i -E 's|^([[:space:]]*)(REGENTE_GIT_SOURCE=.*)$|\1# \2|; s|^([[:space:]]*)(REGENTE_GIT_REPO=.*)$|\1# \2|' "$ENV_FILE" || true
  echo "   offline mode: no GitOps (definitions stay on local disk only)."
fi
if [ -n "$repo" ]; then
  echo ""
  echo "-- GitHub PAT: needed to write to $repo (fine-grained: Contents read/write on that repo,"
  echo "   or a classic token with the 'repo' scope). Without it the server can read a public repo"
  echo "   but cannot Publish."
  read -rsp "GitHub PAT (Enter = keep/skip): " pat; echo
  pat="$(clean "$pat")"
  if [ -n "$pat" ]; then
    # O prompt é silencioso: sem este aviso, um valor truncado/errado só se
    # revela lá na frente, como um 404 do clone.
    case "$pat" in
      ghp_*|gho_*|ghs_*|ghu_*|github_pat_*) : ;;
      *) echo "  ! heads-up: this does not look like a GitHub token (they usually start with ghp_ or github_pat_)."
         echo "    Writing it anyway — check the GitOps status at the end." ;;
    esac
    set_env REGENTE_SECRET_GITHUB_TOKEN "$pat"
    echo "  PAT stored through the secrets provider (REGENTE_SECRET_GITHUB_TOKEN) — it never reaches the DB in plaintext."
  fi
fi

# 3) Domínio público — informativo (usado no passo do nginx/TLS em deploy/vps/).
# `|| true` nos reads finais: com `set -e`, um EOF no stdin (terminal fechado,
# respostas vindas de um pipe) mataria o script DEPOIS de gravar a config e ANTES
# de avisar que falta reiniciar — pego rodando o configure num container.
read -rp "Public domain (e.g. regente.yourcompany.com) [optional]: " dom || true
dom="$(clean "${dom:-}")"
# Colar a URL inteira aqui é o erro natural — e certbot recusa esquema e barra.
# Normalizamos em vez de reclamar, mas dizemos o que fizemos.
case "$dom" in
  http://*|https://*)
    dom="${dom#http://}"; dom="${dom#https://}"; dom="${dom%%/*}"
    echo "   (normalised to the hostname: $dom — certbot does not take a scheme or a trailing slash)" ;;
  */*) dom="${dom%%/*}"; echo "   (normalised to the hostname: $dom)" ;;
esac

echo ""
echo "Config written to $ENV_FILE (perms $(stat -c %a "$ENV_FILE" 2>/dev/null || echo 0640))."
echo "It only takes effect after a restart: sudo systemctl restart regente-server"
read -rp "Restart regente-server now? [Y/n]: " r || true
case "${r:-Y}" in
  [Nn]*) echo "ok — restart it later:  sudo systemctl restart regente-server" ;;
  *)
    systemctl restart regente-server && echo "regente-server restarted."
    # Mostra o resultado REAL do GitOps: se o repo/PAT estiverem errados o server
    # sobe assim mesmo (não é mais fatal), então quem configurou precisa ver aqui.
    if [ -n "$repo" ]; then
      sleep 2
      port="$(grep -E '^REGENTE_ADDR=' "$ENV_FILE" | tail -1 | cut -d= -f2- | sed 's/.*://')"
      port="${port:-8080}"
      if command -v curl >/dev/null; then
        st="$(curl -fsS --max-time 5 -H "Authorization: Bearer $tok" "http://127.0.0.1:${port}/api/git/status" 2>/dev/null || true)"
        # A chave "error" vem SEMPRE no JSON (vazia quando está tudo bem), então
        # casar com '"error"' dizia "NOT connected" até no caminho feliz. O que
        # separa os dois casos é o VALOR: "error":"" = sincronizado.
        case "$st" in
          *'"error":""'*) echo "GitOps connected — workspace synced with $repo." ;;
          *'"error":"'*)  echo ""; echo "!! GitOps NOT connected yet: $st"
                          echo "   the server is running anyway — fix the repo/PAT and it reconnects on its own (no restart needed)." ;;
          *)              echo "GitOps status unavailable — check: journalctl -u regente-server -n 30 --no-pager" ;;
        esac
      fi
    fi
    ;;
esac
if [ -n "$dom" ]; then
  VPS_DIR=/var/lib/regente/deploy/vps
  echo ""
  echo "Domain '$dom' — for HTTPS follow $VPS_DIR/README.md, in this order:"
  # O passo do DNS vem PRIMEIRO de propósito: com o domínio ainda apontando para
  # a página de estacionamento do registrador, o certbot falha com uma mensagem
  # que não menciona DNS em lugar nenhum, e o operador caça o problema errado.
  echo "  1) point the domain at THIS machine and confirm it (an A record, not the registrar's parking page):"
  echo "       getent hosts $dom      # must answer with this server's public IP"
  echo "  2) nginx:   sudo cp $VPS_DIR/nginx-regente.conf /etc/nginx/conf.d/regente.conf"
  echo "              sudo sed -i 's/REGENTE_DOMAIN/$dom/' /etc/nginx/conf.d/regente.conf && sudo nginx -t && sudo systemctl reload nginx"
  echo "  3) TLS:     sudo DOMAIN=$dom EMAIL=<your-email> bash $VPS_DIR/enable-tls.sh"
  echo "  4) close the back door — the proxy talks over the loopback, so the server should not listen publicly:"
  echo "       sudo sed -i 's|^REGENTE_ADDR=.*|REGENTE_ADDR=127.0.0.1:8080|' $ENV_FILE && sudo systemctl restart regente-server"
fi
echo "Done."
