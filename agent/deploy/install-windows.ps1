# Instala o regente-agent como tarefa agendada do Windows (roda no boot/logon e
# reinicia se cair). Mais simples que um serviço Windows nativo e não precisa de
# wrapper (NSSM). Rode num PowerShell **como Administrador**.
#
# Uso:
#   .\install-windows.ps1 -Server ws://host:8080/ws/agent -Token rgta_xxx `
#                         -Id meu-pc -Caps COMMAND,SCRIPT,HTTP
param(
  [Parameter(Mandatory = $true)][string]$Server,
  [Parameter(Mandatory = $true)][string]$Token,
  [string]$Id = $env:COMPUTERNAME,
  [string]$Caps = "COMMAND,SCRIPT,HTTP"
)

$ErrorActionPreference = "Stop"
$src = Join-Path $PSScriptRoot "..\regente-agent.exe"
if (-not (Test-Path $src)) {
  throw "binário não encontrado em $src — rode: (cd ..; go build -o regente-agent.exe .)"
}

$dstDir = "C:\Program Files\Regente"
New-Item -ItemType Directory -Force -Path $dstDir | Out-Null
Copy-Item $src (Join-Path $dstDir "regente-agent.exe") -Force

$exe = Join-Path $dstDir "regente-agent.exe"
$args = "-server $Server -token $Token -id $Id -caps $Caps"

$action  = New-ScheduledTaskAction -Execute $exe -Argument $args
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries

Register-ScheduledTask -TaskName "RegenteAgent" -Action $action -Trigger $trigger `
  -Principal $principal -Settings $settings -Force | Out-Null

Start-ScheduledTask -TaskName "RegenteAgent"
Write-Host "OK — RegenteAgent registrado e iniciado. Ver: Get-ScheduledTask RegenteAgent"
