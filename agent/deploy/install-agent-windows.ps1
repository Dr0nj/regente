<#
  install-agent-windows.ps1 — instalador AMISTOSO do regente-agent no Windows.

  Baixa o .exe da última release e registra como Tarefa Agendada 24/7: inicia no boot,
  reinicia sozinho se cair, roda como SYSTEM (sem ninguém logado). NÃO precisa de Docker,
  Go nem runtime — é um binário estático. Rode num PowerShell COMO ADMINISTRADOR.

  Interativo (pergunta servidor + token):
    irm https://github.com/Dr0nj/regente/releases/latest/download/install-agent-windows.ps1 | iex

  Silencioso (frota / GPO):
    .\install-agent-windows.ps1 -Server wss://regente.suaempresa.com/ws/agent -Token rgta_xxx
#>
param(
  [string]$Server,
  [string]$Token,
  [string]$Id      = $env:COMPUTERNAME,
  [string]$Caps    = "COMMAND,SCRIPT,HTTP",
  [string]$Repo    = "Dr0nj/regente",
  [string]$Version = "latest"
)
$ErrorActionPreference = "Stop"

# Precisa de admin (registrar Tarefa como SYSTEM/boot).
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
         ).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)
if (-not $admin) { Write-Host "Rode num PowerShell COMO ADMINISTRADOR." -ForegroundColor Red; return }

# Pergunta o que faltar.
if (-not $Server) { $Server = Read-Host "URL do servidor (ex.: wss://regente.suaempresa.com/ws/agent)" }
if (-not $Token) {
  $sec = Read-Host "Token do agente (Settings -> Agentes -> Criar token)" -AsSecureString
  $Token = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
             [Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
}
if (-not $Server -or -not $Token) { Write-Host "Servidor e Token sao obrigatorios." -ForegroundColor Red; return }

# Só há binário Windows amd64 na release.
if (-not [Environment]::Is64BitOperatingSystem) { Write-Host "Requer Windows 64-bit (amd64)." -ForegroundColor Red; return }
$asset = "regente-agent_windows_amd64.exe"
$url = if ($Version -eq "latest") { "https://github.com/$Repo/releases/latest/download/$asset" }
       else { "https://github.com/$Repo/releases/download/$Version/$asset" }

# TLS 1.2 (GitHub) no Windows PowerShell 5.1.
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$dstDir = "C:\Program Files\Regente"
New-Item -ItemType Directory -Force -Path $dstDir | Out-Null
$exe = Join-Path $dstDir "regente-agent.exe"
Write-Host "Baixando $asset ($Version)..."
Invoke-WebRequest -Uri $url -OutFile $exe -UseBasicParsing

# Tarefa Agendada: boot + SYSTEM + auto-restart (mesma config do install-windows.ps1).
$argline   = "-server $Server -token $Token -id $Id -caps $Caps"
$action    = New-ScheduledTaskAction -Execute $exe -Argument $argline
$trigger   = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
$settings  = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
             -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName "RegenteAgent" -Action $action -Trigger $trigger `
  -Principal $principal -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName "RegenteAgent"

Write-Host "OK - RegenteAgent instalado e iniciado (boot + auto-restart, como SYSTEM)." -ForegroundColor Green
Write-Host "Status:  Get-ScheduledTask RegenteAgent    |    Confira '$Id' online em Settings -> Agentes."
