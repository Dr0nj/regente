# Instala o regente-server como tarefa agendada do Windows (roda no boot e
# reinicia sozinho se cair). R1 — supervisor do control plane. Mais simples que
# um serviço Windows nativo e sem wrapper (NSSM). Rode como Administrador.
#
# Uso:
#   .\install-windows.ps1 `
#       -Db "C:\ProgramData\Regente\regente.db" `
#       -GitSource https://github.com/<owner>/<seu-workspace>.git
#
# -GitSource é o SEU repositório de workspace — um por instalação, nunca um
# default compartilhado. Pode ser um repo VAZIO recém-criado no GitHub: o server
# escreve o conteúdo inicial e publica. Sem -GitSource roda offline (só disco).
#
# Token do GitHub: prefira definir REGENTE_SECRET_GITHUB_TOKEN no ambiente da
# máquina (System) a colar em texto; ou configure pela UI (Settings → GitHub).
param(
  [string]$Config = "C:\ProgramData\Regente\server.env",
  [string]$Addr = "",
  [string]$DbDriver = "sqlite",
  [string]$Db = "C:\ProgramData\Regente\regente.db",
  [string]$Workspace = "C:\ProgramData\Regente\workspace",
  [string]$GitSource = "",
  [string]$GitBranch = "main"
)

$ErrorActionPreference = "Stop"
$src = Join-Path $PSScriptRoot "..\regente-server.exe"
if (-not (Test-Path $src)) {
  throw "binary not found at $src — run: (cd ..; go build -o regente-server.exe .)"
}

$dstDir = "C:\Program Files\Regente"
New-Item -ItemType Directory -Force -Path $dstDir | Out-Null
New-Item -ItemType Directory -Force -Path (Split-Path $Db) | Out-Null
New-Item -ItemType Directory -Force -Path $Workspace | Out-Null
Copy-Item $src (Join-Path $dstDir "regente-server.exe") -Force

$exe = Join-Path $dstDir "regente-server.exe"
if (-not (Test-Path -LiteralPath $Config)) {
  New-Item -ItemType Directory -Force -Path (Split-Path $Config) | Out-Null
  "`n`n" | & $exe -init-config $Config
  if ($LASTEXITCODE -ne 0) { throw "Private configuration generation failed" }
  Add-Content -LiteralPath $Config -Encoding utf8 -Value "REGENTE_DB_DRIVER=$DbDriver`nREGENTE_DB=$Db`nREGENTE_WORKSPACE=$Workspace"
}
$checkArgs = @('-config-env', $Config, '-check-config')
$argline = "-config-env `"$Config`""
foreach ($pair in @(@('Addr','addr'), @('DbDriver','db-driver'), @('Db','db'), @('Workspace','workspace'))) {
  if ($PSBoundParameters.ContainsKey($pair[0])) {
    $value = (Get-Variable -Name $pair[0] -ValueOnly)
    if ($value -match '["\r\n]') { throw "Invalid argument value" }
    $argline += " -$($pair[1]) `"$value`""
    $checkArgs += @("-$($pair[1])", $value)
  }
}
& $exe @checkArgs
if ($LASTEXITCODE -ne 0) { throw "Configuration rejected; no scheduled task changed" }
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
Write-Host "Read the bootstrap password privately from $Config; remove it after first login."
Write-Host "OK — RegenteServer registered and started (it restarts itself). Check it: Get-ScheduledTask RegenteServer"
