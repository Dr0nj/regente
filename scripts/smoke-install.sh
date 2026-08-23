#!/usr/bin/env bash
# smoke-install.sh — instala o Regente num Linux com systemd DE VERDADE
# (container) e exercita o circuito inteiro do operador. É o portão da release:
# `release.yml` roda isto ANTES de publicar, então uma instalação quebrada não
# vira `latest`.
#
# Por que existe: os dois piores defeitos da trilha de instalação (clone falho
# deixando .git parcial; push que não publica nada) passaram por go build, go
# test, staticcheck e lint — e só apareceram instalando de verdade.
#
# Uso:
#   bash scripts/smoke-install.sh --build                  # builda do checkout e testa
#   bash scripts/smoke-install.sh --bundle X.tar.gz --agent ./regente-agent
#
# Requisitos: docker. Com --build também Go 1.25+ e Node 18+.
set -euo pipefail

# Git Bash/MSYS reescreve caminhos estilo Unix nos argumentos de programas
# nativos: `-v /sys/fs/cgroup:...` vira `C:\Program Files\Git\sys...` e o docker
# recusa. A desativação vale SÓ para o docker — global ela quebraria o `go build
# -o /c/...`, que PRECISA da conversão para receber um caminho Windows válido.
dk() { MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' docker "$@"; }

# ...só que aí os caminhos do HOST (contexto do build, origem do `docker cp`)
# chegam como /c/Users/..., que o docker do Windows não resolve. A conversão
# desses passa a ser explícita. No Linux (CI) não há cygpath e vira no-op.
hostpath() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi
}

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CT="regente-smoke-$$"
IMG="regente-smoke-systemd"
BUNDLE=""
AGENT=""
BUILD=0
KEEP="${KEEP:-0}"   # KEEP=1 deixa o container de pé pra investigar uma falha

while [ $# -gt 0 ]; do
  case "$1" in
    --build)  BUILD=1; shift ;;
    --bundle) BUNDLE="$2"; shift 2 ;;
    --agent)  AGENT="$2"; shift 2 ;;
    --keep)   KEEP=1; shift ;;
    *) echo "argumento desconhecido: $1"; exit 2 ;;
  esac
done

command -v docker >/dev/null || { echo "docker é necessário"; exit 2; }

cleanup() {
  local rc=$?
  if [ "$KEEP" = 1 ]; then
    echo "== container mantido de pé: docker exec -it $CT bash  (docker rm -f $CT quando terminar)"
  else
    dk rm -f "$CT" >/dev/null 2>&1 || true
  fi
  exit $rc
}
trap cleanup EXIT

if [ "$BUILD" = 1 ]; then
  echo "== buildando bundle a partir do checkout..."
  rm -rf "$ROOT/.smoke"; mkdir -p "$ROOT/.smoke/stage/regente-server_linux_amd64/deploy" "$ROOT/.smoke/stage/regente-server_linux_amd64/app"
  (cd "$ROOT/app" && VITE_REGENTE_SERVER_URL=@origin npm ci --silent && npm run build >/dev/null)
  # -X main.version: espelha o que o release.yml faz. Com um valor RECONHECÍVEL,
  # o inside.sh consegue provar a cadeia inteira (ldflags → binário → /api/version
  # → rodapé da UI) em vez de só checar que o campo existe.
  (cd "$ROOT/server" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-X main.version=smoke-test" -o "$ROOT/.smoke/stage/regente-server_linux_amd64/regente-server" .)
  (cd "$ROOT/agent" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$ROOT/.smoke/regente-agent" .)
  cp -r "$ROOT/app/dist" "$ROOT/.smoke/stage/regente-server_linux_amd64/app/dist"
  cp "$ROOT/server/deploy/install-linux.sh" "$ROOT/server/deploy/configure.sh" "$ROOT/server/deploy/update.sh" \
     "$ROOT/server/deploy/regente-server.service" "$ROOT/server/deploy/server.env.example" \
     "$ROOT/.smoke/stage/regente-server_linux_amd64/deploy/"
  # Espelha o release.yml: a borda (nginx/TLS/sandbox) também viaja no bundle, e
  # quem a instala em /var/lib/regente/deploy/vps é o install-linux.sh. Sem isto
  # o smoke testaria um bundle diferente do que a CI publica.
  cp -r "$ROOT/deploy/vps" "$ROOT/.smoke/stage/regente-server_linux_amd64/deploy/vps"
  # O release.yml faz `chmod +x` antes de empacotar; num checkout Windows o bit
  # de execução não persiste no NTFS, então aqui o modo é forçado no próprio tar
  # (o installer recusa — corretamente — um bundle sem binário executável).
  # Vale SÓ para este modo --build: um bundle vindo da CI é testado como veio.
  chmod +x "$ROOT/.smoke/stage/regente-server_linux_amd64/regente-server" "$ROOT/.smoke/regente-agent" 2>/dev/null || true
  # Num checkout Windows os arquivos de deploy vêm com CRLF, e o \r vaza pra
  # dentro do sistema instalado: o EnvironmentFile do systemd passa a carregar
  # "change-me\r" como token e TODA chamada à API volta 400 (header inválido).
  # A CI empacota de um checkout Linux; aqui normalizamos pra o bundle de teste
  # ficar igual ao publicado.
  sed -i 's/\r$//' "$ROOT/.smoke/stage/regente-server_linux_amd64/deploy/"* \
                   "$ROOT/.smoke/stage/regente-server_linux_amd64/deploy/vps/"* 2>/dev/null || true
  tar --mode=0755 -C "$ROOT/.smoke/stage" -czf "$ROOT/.smoke/bundle.tar.gz" regente-server_linux_amd64
  BUNDLE="$ROOT/.smoke/bundle.tar.gz"
  AGENT="$ROOT/.smoke/regente-agent"
fi

[ -f "$BUNDLE" ] || { echo "bundle não encontrado: '$BUNDLE' (use --build ou --bundle <arquivo>)"; exit 2; }
[ -f "$AGENT" ]  || { echo "binário do agente não encontrado: '$AGENT'"; exit 2; }

echo "== imagem do alvo (Ubuntu + systemd)..."
dk build -q -t "$IMG" "$(hostpath "$ROOT/scripts/smoke")" >/dev/null

echo "== subindo o 'VPS'..."
dk run -d --name "$CT" --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw --tmpfs /run --tmpfs /run/lock \
  "$IMG" >/dev/null

for i in $(seq 1 60); do
  st="$(dk exec "$CT" systemctl is-system-running 2>/dev/null || true)"
  case "$st" in running|degraded) break ;; esac
  sleep 1
done
case "${st:-}" in
  running|degraded) ;;
  *) echo "systemd não subiu no container (estado: ${st:-vazio})"; dk logs "$CT" | tail -20; exit 1 ;;
esac

dk cp "$(hostpath "$BUNDLE")" "$CT:/root/bundle.tar.gz" >/dev/null
dk cp "$(hostpath "$AGENT")" "$CT:/root/regente-agent" >/dev/null
dk exec "$CT" mkdir -p /root/agent-deploy
dk cp "$(hostpath "$ROOT/agent/deploy/install-linux.sh")" "$CT:/root/agent-deploy/install-linux.sh" >/dev/null
dk cp "$(hostpath "$ROOT/agent/deploy/regente-agent.service")" "$CT:/root/agent-deploy/regente-agent.service" >/dev/null
dk exec "$CT" chmod +x /root/regente-agent
# Checkout Windows entrega esses arquivos com CRLF; dentro do container o \r
# quebra o script e faz o systemd ignorar linhas da unit (o agente sobe com id e
# capabilities DEFAULT, sem os que foram passados). Na CI já vêm com LF — este
# passo é só pra o smoke rodar igual na máquina do dev.
dk cp "$(hostpath "$ROOT/scripts/smoke/inside.sh")" "$CT:/root/inside.sh" >/dev/null
dk exec "$CT" bash -c 'sed -i "s/\r$//" /root/agent-deploy/install-linux.sh /root/agent-deploy/regente-agent.service /root/inside.sh'

echo "== stage 1: instalar, quebrar, consertar, executar"
if ! dk exec "$CT" bash /root/inside.sh stage1; then
  echo; echo "---- journal do server ----"; dk exec "$CT" journalctl -u regente-server --no-pager -n 60 || true
  echo; echo "---- journal do agente ----"; dk exec "$CT" journalctl -u regente-agent --no-pager -n 30 || true
  exit 1
fi

echo
echo "== reboot da máquina (docker restart) e stage 2: o estado sobreviveu?"
dk restart "$CT" >/dev/null
for i in $(seq 1 60); do
  st="$(dk exec "$CT" systemctl is-system-running 2>/dev/null || true)"
  case "$st" in running|degraded) break ;; esac
  sleep 1
done
if ! dk exec "$CT" bash /root/inside.sh stage2; then
  echo; echo "---- journal do server ----"; dk exec "$CT" journalctl -u regente-server --no-pager -n 60 || true
  exit 1
fi

echo
echo "✅ smoke de instalação OK — o artefato instala, sobrevive a config errada e a reboot, e executa job."
