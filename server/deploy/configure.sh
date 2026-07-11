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
[ "$(id -u)" = 0 ]  || { echo "rode como root (sudo)"; exit 1; }
[ -t 0 ]            || { echo "precisa de terminal interativo — rode 'sudo regente-configure' direto (não via pipe)."; exit 1; }
[ -f "$ENV_FILE" ]  || { echo "$ENV_FILE não existe — rode o install primeiro (install-linux.sh)."; exit 1; }

gen_token() {
  if command -v openssl >/dev/null 2>&1; then openssl rand -hex 24
  else tr -dc 'a-f0-9' </dev/urandom | head -c 48; echo; fi
}
current() { grep -E "^[[:space:]]*${1}=" "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2- || true; }
set_env() {  # set_env KEY VALUE  (upsert idempotente; descomenta se preciso)
  local key="$1" val="$2" esc
  esc=$(printf '%s' "$val" | sed -e 's/[&|]/\\&/g')
  if grep -qE "^[[:space:]]*#?[[:space:]]*${key}=" "$ENV_FILE"; then
    sed -i -E "s|^[[:space:]]*#?[[:space:]]*${key}=.*|${key}=${esc}|" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$val" >> "$ENV_FILE"
  fi
}

echo "== Configuração guiada do Regente — Enter mantém o valor sugerido/atual =="
echo ""

# 1) Token de API — admin-equivalente (bypassa login). NUNCA deixe dev-token/change-me.
cur_tok="$(current REGENTE_TOKEN)"
sug_tok="$cur_tok"
case "$sug_tok" in ''|dev-token|change-me) sug_tok="$(gen_token)";; esac
read -rp "REGENTE_TOKEN (token de API forte) [$sug_tok]: " tok
tok="${tok:-$sug_tok}"
set_env REGENTE_TOKEN "$tok"

# 2) GitHub — repo do workspace (GitOps) + PAT via secrets provider (fora da DB em claro).
cur_repo="$(current REGENTE_GIT_REPO)"
read -rp "Repo do workspace GitOps (owner/name) [${cur_repo:-Dr0nj/regente-workspace}]: " repo
repo="${repo:-${cur_repo:-Dr0nj/regente-workspace}}"
if [ -n "$repo" ]; then
  set_env REGENTE_GIT_REPO "$repo"
  set_env REGENTE_GIT_SOURCE "https://github.com/${repo}.git"
fi
read -rsp "GitHub PAT (push/PR + clone privado; Enter = manter/omitir): " pat; echo
if [ -n "$pat" ]; then
  set_env REGENTE_SECRET_GITHUB_TOKEN "$pat"
  echo "  PAT gravado via secrets provider (REGENTE_SECRET_GITHUB_TOKEN) — não vai pra DB em claro."
fi

# 3) Domínio público — informativo (usado no passo do nginx/TLS em deploy/vps/).
read -rp "Domínio público (ex.: regente.suaempresa.com) [opcional]: " dom

echo ""
echo "Config escrita em $ENV_FILE (perms $(stat -c %a "$ENV_FILE" 2>/dev/null || echo 0640))."
read -rp "Reiniciar o regente-server agora? [S/n]: " r
case "${r:-S}" in
  [Nn]*) echo "ok — reinicie depois:  sudo systemctl restart regente-server" ;;
  *)     systemctl restart regente-server && echo "regente-server reiniciado." ;;
esac
if [ -n "$dom" ]; then
  echo ""
  echo "Domínio '$dom': siga deploy/vps/ pra HTTPS —"
  echo "  sudo DOMAIN=$dom EMAIL=voce@dominio.com ./deploy/vps/enable-tls.sh"
fi
echo "Pronto."
