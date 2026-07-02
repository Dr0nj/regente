<#
  host-demo.ps1 — sobe a demo do Regente num link público pra amigos testarem a UI
  (criar E executar jobs), no seu PC, com:
    - servidor Go servindo o SPA (single-origin: UI+API+WS numa porta só)
    - GitOps DIRETO no seu repo real (github.com/Dr0nj/regente-workspace)
    - agente em CONTAINER Docker descartável (jobs rodam isolados, sem tocar sua máquina)
    - Cloudflare Tunnel (link https público, grátis, sem conta)

  Uso (PowerShell, na raiz do repo):
    .\deploy\demo\host-demo.ps1

  Pré-requisitos: Go 1.25+, Node/npm, Docker Desktop LIGADO, cloudflared no PATH.
  (cloudflared: winget install --id Cloudflare.cloudflared  — ou baixe o .exe.)

  Pra PARAR: feche esta janela (Ctrl+C no cloudflared) e rode:
    docker rm -f regente-sandbox ; Get-Process regente-server -EA SilentlyContinue | Stop-Process -Force
#>

param(
  [int]$Port = 9091,
  [string]$Token = "demo-" + -join ((48..57 + 97..122) | Get-Random -Count 12 | ForEach-Object { [char]$_ }),
  [string]$GitRepo = "Dr0nj/regente-workspace",
  [string]$GitHubToken = ""
)

# NÃO usamos ErrorActionPreference=Stop: comandos nativos (docker, cloudflared) escrevem
# no stderr como parte NORMAL da operação (ex.: docker "No such container", logs do
# cloudflared) e o Stop trataria isso como erro fatal, abortando o script. Em vez disso,
# checamos $LASTEXITCODE só nos passos que PRECISAM ter dado certo.
$repo = (Resolve-Path "$PSScriptRoot\..\..").Path
function Assert-Ok($msg) {
  if ($LASTEXITCODE -ne 0) {
    Write-Host "`nFALHOU: $msg (exit $LASTEXITCODE)" -ForegroundColor Red
    exit 1
  }
}
Write-Host "== Regente demo host ==  repo=$repo  porta=$Port" -ForegroundColor Cyan

# Re-run limpo: derruba uma instância anterior desta demo (server + container), se houver,
# pra a porta e o nome do container ficarem livres.
Get-Process regente-server -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
docker rm -f regente-sandbox *>$null

# --- 0) GitHub PAT (push direto no workspace) — reusa o salvo pelo launcher, senão pede.
if (-not $GitHubToken) {
  $saved = Join-Path $env:LOCALAPPDATA "regente-lab\github-token.txt"
  if (Test-Path $saved) { $GitHubToken = (Get-Content $saved -Raw).Trim() }
}
if (-not $GitHubToken) {
  $GitHubToken = Read-Host "Cole um GitHub PAT com permissao de PUSH em $GitRepo (Enter = so leitura/dry-run)"
}

# --- 1) Build do frontend (same-origin: @origin -> window.location.origin em runtime).
Write-Host "`n[1/5] Buildando o frontend (VITE_REGENTE_SERVER_URL=@origin)..." -ForegroundColor Yellow
Push-Location (Join-Path $repo "app")
$env:VITE_REGENTE_SERVER_URL = "@origin"
npm run build
Pop-Location
Assert-Ok "build do frontend (npm run build)"
$dist = Join-Path $repo "app\dist"

# --- 2) Build do servidor.
Write-Host "`n[2/5] Buildando o regente-server..." -ForegroundColor Yellow
Push-Location (Join-Path $repo "server")
go build -o (Join-Path $repo "regente-server.exe") .
Pop-Location
Assert-Ok "build do regente-server (go build)"

# --- 3) Sobe o servidor (single-origin + GitOps direto no repo real).
Write-Host "`n[3/5] Subindo o servidor em :$Port (SPA + API + WS)..." -ForegroundColor Yellow
$dataDir = Join-Path $env:LOCALAPPDATA "regente-demo"
New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
$srvArgs = @(
  "-addr", ":$Port",
  "-spa-dir", $dist,
  "-workspace", (Join-Path $dataDir "ws-git"),
  "-db", (Join-Path $dataDir "regente.db"),
  "-git-source", "https://github.com/$GitRepo.git",
  "-git-write-mode", "direct",
  "-github-repo", $GitRepo,
  "-git-commit",
  "-api-token", $Token
)
if ($GitHubToken) { $srvArgs += @("-github-token", $GitHubToken) }
$srv = Start-Process -FilePath (Join-Path $repo "regente-server.exe") -ArgumentList $srvArgs -PassThru
Start-Sleep -Seconds 3
Write-Host "   servidor PID $($srv.Id)  (token de API: $Token)" -ForegroundColor DarkGray

# --- 4) Agente em container Docker descartável (sandbox — jobs rodam isolados).
Write-Host "`n[4/5] Buildando + subindo o agente em Docker (sandbox)..." -ForegroundColor Yellow
docker build -f (Join-Path $repo "deploy\demo\Dockerfile.agent") -t regente-agent:demo (Join-Path $repo "agent")
Assert-Ok "build da imagem do agente (docker build)"
# Limpeza best-effort: se o container não existe, o erro é benigno — ignoramos.
docker rm -f regente-sandbox *>$null
docker run -d --name regente-sandbox --rm `
  --cap-drop ALL --security-opt no-new-privileges `
  --pids-limit 256 --memory 512m --cpus 1 `
  regente-agent:demo `
  -server "ws://host.docker.internal:$Port/ws/agent" `
  -token $Token -id docker-sandbox -caps "COMMAND,SCRIPT,HTTP"
Assert-Ok "subir o agente (docker run)"
Write-Host "   agente 'docker-sandbox' conectado (jobs COMMAND/SCRIPT/HTTP rodam DENTRO do container)" -ForegroundColor DarkGray

# --- 5) Cloudflare Tunnel — link https publico (grátis, sem conta).
Write-Host "`n[5/5] Abrindo o Cloudflare Tunnel (o link aparece abaixo, procure trycloudflare.com)..." -ForegroundColor Yellow
if (-not (Get-Command cloudflared -ErrorAction SilentlyContinue)) {
  Write-Host "cloudflared nao encontrado. Instale com:  winget install --id Cloudflare.cloudflared" -ForegroundColor Red
  Write-Host "O servidor local ja esta no ar em http://localhost:$Port (login inicial: admin / admin)." -ForegroundColor Green
  exit 1
}
Write-Host "`n>> Compartilhe o link https://<...>.trycloudflare.com que aparecer. Login inicial: admin / admin" -ForegroundColor Green
Write-Host ">> Crie contas pros amigos em Configuracoes -> Usuarios (role operator pra criar/rodar; viewer so olha).`n" -ForegroundColor Green
cloudflared tunnel --url "http://localhost:$Port"
