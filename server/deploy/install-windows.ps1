# Instala o regente-server como tarefa agendada do Windows (roda no boot e
# reinicia sozinho se cair). R1 — supervisor do control plane. Mais simples que
# um serviço Windows nativo e sem wrapper (NSSM). Rode como Administrador.
#
# Uso:
#   .\install-windows.ps1 -Token <token-forte> `
#       -Db "C:\ProgramData\Regente\regente.db" `
#       -GitSource https://github.com/Dr0nj/regente-workspace.git
#
# Token do GitHub: prefira definir REGENTE_SECRET_GITHUB_TOKEN no ambiente da
# máquina (System) a colar em texto; ou configure pela UI (Settings → GitHub).
param(
  [Parameter(Mandatory = $true)][string]$Token,
  [string]$Addr = ":8080",
  [string]$DbDriver = "sqlite",
  [string]$Db = "C:\ProgramData\Regente\regente.db",
  [string]$Workspace = "C:\ProgramData\Regente\workspace",
  [string]$GitSource = "",
  [string]$GitBranch = "main"
)

$ErrorActionPreference = "Stop"
$src = Join-Path $PSScriptRoot "..\regente-server.exe"
if (-not (Test-Path $src)) {
  throw "binário não encontrado em $src — rode: (cd ..; go build -o regente-server.exe .)"
}

$dstDir = "C:\Program Files\Regente"
New-Item -ItemType Directory -Force -Path $dstDir | Out-Null
New-Item -ItemType Directory -Force -Path (Split-Path $Db) | Out-Null
New-Item -ItemType Directory -Force -Path $Workspace | Out-Null
Copy-Item $src (Join-Path $dstDir "regente-server.exe") -Force

$exe = Join-Path $dstDir "regente-server.exe"
$argline = "-addr $Addr -db-driver $DbDriver -db `"$Db`" -workspace `"$Workspace`" -api-token $Token"
if ($GitSource) { $argline += " -git-source $GitSource -git-branch $GitBranch" }

$action  = New-ScheduledTaskAction -Execute $exe -Argument $argline
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
# Reinício automático se o processo cair (até 999x, intervalo de 1 min).
$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
  -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero)

Register-ScheduledTask -TaskName "RegenteServer" -Action $action -Trigger $trigger `
  -Principal $principal -Settings $settings -Force | Out-Null

Start-ScheduledTask -TaskName "RegenteServer"
Write-Host "OK — RegenteServer registrado e iniciado (reinicia sozinho). Ver: Get-ScheduledTask RegenteServer"
