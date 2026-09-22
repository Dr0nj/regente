<#
  Demo local: provisiona identidade, verifica presenca e COMMAND real.
  Nunca abre tunel. Cada execucao possui DB/workspace/processos/container proprios.
  -NativeSmoke so permite o COMMAND sintetico no host, nunca sessao de convidados.
#>
[CmdletBinding()]
param(
  [ValidateRange(0, 65535)][int]$Port = 9091,
  [string]$BindAddress = '127.0.0.1',
  [string]$GitRepo = '',
  [string]$GitBranch = 'main',
  [switch]$Smoke,
  [switch]$NativeSmoke,
  [switch]$GitFixture,
  [string]$EvidencePath = '',
  [ValidateRange(5, 180)][int]$TimeoutSeconds = 45
)
$ErrorActionPreference = 'Stop'
$onWindows = $env:OS -eq 'Windows_NT'
if ($NativeSmoke -and -not $Smoke) { throw '-NativeSmoke requires -Smoke; it is not a guest execution mode.' }
if ($GitFixture -and -not $Smoke) { throw '-GitFixture requires -Smoke.' }
if ($Smoke -and $GitRepo) { throw 'Smoke tests cannot access a real GitHub workspace.' }
if ($EvidencePath -and -not $Smoke) { throw '-EvidencePath requires -Smoke.' }
if ($EvidencePath -and (Test-Path -LiteralPath $EvidencePath)) { throw 'EvidencePath must be a new file.' }
if ($GitRepo -and $GitRepo -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') { throw 'GitRepo must be owner/repository.' }
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$runId = [Guid]::NewGuid().ToString('N')
$runDir = Join-Path ([IO.Path]::GetTempPath()) "regente-demo-$runId"
$container = "regente-demo-$runId"
$imageName = "regente-agent:demo-$runId"
$srv = $null; $agent = $null; $containerOwned = $false; $imageOwned = $false
$credential = $null; $base = ''; $headers = @{}
$savedEnv = @{}
# Nao herdar credenciais/configuracoes pessoais; restaurar o ambiente do chamador.
@(Get-ChildItem Env:) | Where-Object { $_.Name -match '^(REGENTE_|VITE_|OTEL_|GITHUB_TOKEN$|GH_TOKEN$)' } | ForEach-Object {
  $savedEnv[$_.Name] = $_.Value
  [Environment]::SetEnvironmentVariable($_.Name, $null, 'Process')
}
function Invoke-Native([string]$Command, [string[]]$Arguments) {
  & $Command @Arguments
  if ($LASTEXITCODE -ne 0) { throw "$Command failed (exit $LASTEXITCODE)." }
}
function Start-Owned([string]$Binary, [string[]]$Arguments, [string]$Name) {
  # Start-Process junta ArgumentList; paths precisam de aspas no Windows 5.1.
  $quoted = @($Arguments | ForEach-Object { '"' + ($_ -replace '"', '\"') + '"' })
  $options = @{ FilePath=$Binary; ArgumentList=$quoted; WorkingDirectory=$runDir; PassThru=$true
    RedirectStandardOutput=(Join-Path $runDir "$Name.out.log"); RedirectStandardError=(Join-Path $runDir "$Name.err.log") }
  if ($onWindows) { $options.WindowStyle = 'Hidden' }
  Start-Process @options
}
function Api([string]$Path, [string]$Method = 'GET', $Body = $null) {
  $options = @{ Uri="$base$Path"; Method=$Method; Headers=$headers; TimeoutSec=5; UseBasicParsing=$true }
  if ($null -ne $Body) { $options.Body = ConvertTo-Json -InputObject $Body -Depth 12 -Compress; $options.ContentType='application/json' }
  Invoke-RestMethod @options
}
function Wait-For([string]$Description, [scriptblock]$Check) {
  $until = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
  do {
    if ($srv -and $srv.HasExited) { throw 'The owned server exited; inspect the private run directory.' }
    if (& $Check) { return }
    Start-Sleep -Milliseconds 250
  } while ([DateTime]::UtcNow -lt $until)
  throw "Timed out waiting for $Description ($TimeoutSeconds seconds)."
}
function Denied([string]$Secret, [string]$Claims, [int]$Expected) {
  $actual = 0
  try { $null = Invoke-WebRequest -UseBasicParsing -Uri "$base/api/agent/poll?$Claims" -Headers @{Authorization="Bearer $Secret"} -TimeoutSec 5 }
  catch {
    if ($_.Exception.Response) { $actual = [int]$_.Exception.Response.StatusCode }
    else { throw 'Negative credential check failed without an HTTP response.' }
  }
  if ($actual -ne $Expected) { throw "Credential rejection expected HTTP $Expected, got $actual." }
}
try {
  New-Item -ItemType Directory -Path $runDir | Out-Null
  Write-Host "Local demo run: $runDir (no public tunnel)"
  if ($Port -eq 0) {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start(); $Port = $listener.LocalEndpoint.Port; $listener.Stop()
  }
  # Nunca matar processo por nome/porta.
  $probe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Parse($BindAddress), $Port)
  $probe.Start(); $probe.Stop()
  if (-not $NativeSmoke) {
    $dockerOS = & docker info --format '{{.OSType}}'
    if ($LASTEXITCODE -ne 0 -or $dockerOS -ne 'linux') { throw 'A running Linux Docker engine is required.' }
  }
  Push-Location (Join-Path $repo 'app')
  try {
    Invoke-Native npm @('ci')
    $env:VITE_REGENTE_SERVER_URL = '@origin'
    Invoke-Native npm @('run', 'build')
  } finally { Pop-Location }
  $suffix = ''; if ($onWindows) { $suffix = '.exe' }
  $serverBin = Join-Path $runDir "server$suffix"
  Invoke-Native go @('-C', (Join-Path $repo 'server'), 'build', '-o', $serverBin, '.')
  $agentID = "demo-$runId"
  $workspace = Join-Path $runDir 'workspace'
  $source = ''
  $fixtureRoot = $workspace
  if ($GitFixture) { $source = Join-Path $runDir 'synthetic-source'; $fixtureRoot = $source }
  if ($Smoke) {
    $defs = Join-Path $fixtureRoot 'definitions/demo'
    New-Item -ItemType Directory -Path $defs -Force | Out-Null
    # JSON e YAML valido; params e o contrato em disco, actionConfig e o da API.
    $definition = @{id='smoke'; label='Synthetic demo smoke'; team='demo'; jobType='COMMAND'; agentId=$agentID
      environment='demo'; schedule=@{enabled=$false}; params=@{command='echo regente-demo-smoke'} }
    [IO.File]::WriteAllText((Join-Path $defs 'smoke.yaml'), (ConvertTo-Json $definition -Depth 8))
    if ($GitFixture) {
      Invoke-Native git @('init', '-b', 'main', $source)
      Invoke-Native git @('-C', $source, 'add', '.')
      Invoke-Native git @('-C', $source, '-c', 'user.name=Demo', '-c', 'user.email=demo@example.invalid', '-c', 'commit.gpgsign=false', 'commit', '-m', 'synthetic fixture')
      $GitBranch = 'main'
    }
  }
  if ($GitRepo) {
    $source = "https://github.com/$GitRepo.git"
    $env:GITHUB_TOKEN = $savedEnv['GITHUB_TOKEN']
  }
  $random = New-Object byte[] 32
  $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
  try { $rng.GetBytes($random) } finally { $rng.Dispose() }
  $adminToken = [Convert]::ToBase64String($random)
  $env:REGENTE_TOKEN = $adminToken
  $srvArgs = @("-addr=$($BindAddress):$Port", '-spa-dir', (Join-Path $repo 'app/dist'),
    '-workspace', $workspace, '-db', (Join-Path $runDir 'state.db'), '-db-driver=sqlite',
    "-git-source=$source", "-git-branch=$GitBranch", "-github-repo=$GitRepo", '-git-poll-interval=0',
    '-auth-mode=local', '-scheduler=external', '-bus=hub', '-demo-mode=false', '-server-agent=false',
    '-selfmon=false', '-design-session-gc-tick-min=0')
  if ($GitRepo) { $srvArgs += @('-git-commit', '-git-write-mode=direct') }
  if (-not $Smoke) { $srvArgs = @($srvArgs | Where-Object { $_ -ne '-scheduler=external' }) }
  $srv = Start-Owned $serverBin $srvArgs 'server'
  $env:REGENTE_TOKEN = $null; $env:GITHUB_TOKEN = $null
  $base = "http://127.0.0.1:$Port"
  $headers = @{Authorization="Bearer $adminToken"}
  Wait-For 'server readiness' { try { $null = Api '/readyz'; $true } catch { $false } }
  if (-not $source) { Write-Host 'Git source is empty: OFFLINE, local definitions only.' }
  $credential = Api '/api/agents/tokens' 'POST' @{label='Local demo'; agentId=$agentID; environment='demo'
    capabilities=@('COMMAND','SCRIPT','HTTP'); expiresAt=[DateTime]::UtcNow.AddHours(8).ToString('o')}
  $claims = "id=$agentID&env=demo&caps=COMMAND,SCRIPT,HTTP"
  if ($Smoke) {
    Denied $adminToken $claims 401
    Denied $credential.token 'id=wrong&env=demo&caps=COMMAND,SCRIPT,HTTP' 403
    Denied $credential.token "id=$agentID&env=wrong&caps=COMMAND,SCRIPT,HTTP" 403
    Denied $credential.token "id=$agentID&env=demo&caps=COMMAND" 403
  }
  $env:REGENTE_TOKEN = $credential.token
  if ($NativeSmoke) {
    $agentBin = Join-Path $runDir "agent$suffix"
    Invoke-Native go @('-C', (Join-Path $repo 'agent'), 'build', '-o', $agentBin, '.')
    $agent = Start-Owned $agentBin @('-server', "ws://127.0.0.1:$Port/ws/agent", '-id', $agentID, '-env=demo', '-caps=COMMAND,SCRIPT,HTTP') 'agent'
  } else {
    Invoke-Native docker @('build', '-f', (Join-Path $repo 'deploy/demo/Dockerfile.agent'), '-t', $imageName, (Join-Path $repo 'agent'))
    $imageOwned = $true
    $networkArgs = @('--network', 'host')
    $agentServer = "ws://127.0.0.1:$Port/ws/agent"
    if ($onWindows) { $networkArgs = @(); $agentServer = "ws://host.docker.internal:$Port/ws/agent" }
    Invoke-Native docker (@('run', '-d', '--name', $container, '--rm', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
      '--pids-limit', '256', '--memory', '512m', '--cpus', '1', '-e', 'REGENTE_TOKEN') + $networkArgs +
      @($imageName, '-server', $agentServer, '-id', $agentID, '-env=demo', '-caps=COMMAND,SCRIPT,HTTP'))
    $containerOwned = $true
  }
  $env:REGENTE_TOKEN = $null
  Wait-For 'authenticated agent presence' {
    $found = @(Api '/api/agents' | Where-Object { $_.id -eq $agentID -and $_.online -and $_.environment -eq 'demo' })
    $found.Count -eq 1
  }
  Write-Host "Agent authenticated: $agentID (environment demo; COMMAND/SCRIPT/HTTP)."
  if ($Smoke) {
    $ordered = Api '/api/folders/demo/order' 'POST' @{}
    if ($ordered.ordered -ne 1) { throw 'Synthetic definition was not loaded/ordered.' }
    $instanceId = $ordered.instanceIds[0]
    Wait-For 'synthetic COMMAND result' {
      $null = Api '/api/scheduler/tick' 'POST' @{}
      $row = Api "/api/instances/$instanceId"
      if ($row.status -in @('NOTOK','CANCELLED')) { throw 'Synthetic COMMAND failed.' }
      $row.status -eq 'OK' -and $row.agentId -eq $agentID -and $row.output -match 'regente-demo-smoke'
    }
    $null = Api "/api/agents/tokens/$($credential.id)" 'DELETE'
    Wait-For 'revoked agent removal' { @(Api '/api/agents' | Where-Object { $_.id -eq $agentID -and $_.online }).Count -eq 0 }
    Denied $credential.token $claims 401
    $result = @{status='passed'; nativeSmoke=[bool]$NativeSmoke; gitFixture=[bool]$GitFixture
      authenticatedPresence=$true; commandCompleted=$true; humanTokenRejected=$true; mismatchedClaimsRejected=$true; revokedTokenRejected=$true
      publicTunnel=$false; node=(& node --version); platform=[Environment]::OSVersion.Platform.ToString()}
    [IO.File]::WriteAllText((Join-Path $runDir 'evidence.json'), (ConvertTo-Json $result))
    if ($EvidencePath) { [IO.File]::WriteAllText([IO.Path]::GetFullPath($EvidencePath), (ConvertTo-Json $result)) }
    Write-Host 'PASS: authenticated COMMAND, claim rejection and revocation; no public resources.'
  } else {
    Write-Host "Open $base (initial login admin / admin; change it immediately)."
    Write-Host 'Use environment demo on guest jobs. Container networking is ON, not job-only isolation.'
    $null = Read-Host 'Press Enter to stop this demo (only owned resources are stopped)'
  }
} finally {
  $env:REGENTE_TOKEN = $null
  if ($credential -and $srv -and -not $srv.HasExited) {
    try { $null = Api "/api/agents/tokens/$($credential.id)" 'DELETE' } catch { Write-Warning 'Credential cleanup failed; the isolated server is being stopped.' }
  }
  if ($containerOwned) { & docker rm -f $container | Out-Null }
  if ($imageOwned) { & docker image rm $imageName | Out-Null }
  foreach ($owned in @($agent, $srv)) {
    if ($owned -and -not $owned.HasExited) { Stop-Process -Id $owned.Id -Force; $owned.WaitForExit() }
  }
  @(Get-ChildItem Env:) | Where-Object { $_.Name -match '^(REGENTE_|VITE_|OTEL_|GITHUB_TOKEN$|GH_TOKEN$)' } | ForEach-Object { [Environment]::SetEnvironmentVariable($_.Name, $null, 'Process') }
  foreach ($key in $savedEnv.Keys) { [Environment]::SetEnvironmentVariable($key, $savedEnv[$key], 'Process') }
  Write-Host "Stopped owned resources. Private data/logs retained at $runDir; do not publish that directory."
}
