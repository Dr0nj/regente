$ErrorActionPreference = 'Stop'
$launcher = Join-Path $PSScriptRoot '../deploy/demo/host-demo.ps1'
function Expect-Rejection($Arguments, [string]$Message) {
  $rejected = $false
  try { & $launcher @Arguments } catch {
    if ($_.Exception.Message -notlike "*$Message*") { throw }
    $rejected = $true
  }
  if (-not $rejected) { throw "Expected launcher rejection: $Message" }
}
Expect-Rejection @{NativeSmoke=$true} '-NativeSmoke requires -Smoke'
Expect-Rejection @{Smoke=$true; GitRepo='owner/real-workspace'} 'cannot access a real GitHub workspace'
Expect-Rejection @{GitFixture=$true} '-GitFixture requires -Smoke'
# Porta de outro processo deve continuar ocupada; nunca matar pelo nome/porta.
$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$before = $env:REGENTE_GIT_SOURCE
try {
  $listener.Start()
  $portNumber = $listener.LocalEndpoint.Port
  $env:REGENTE_GIT_SOURCE = 'https://example.invalid/must-not-clone'
  $rejected = $false
  try { & $launcher -Smoke -NativeSmoke -Port $portNumber } catch {
    $cause = $_.Exception.GetBaseException()
    if ($cause -isnot [Net.Sockets.SocketException] -or $cause.SocketErrorCode -ne [Net.Sockets.SocketError]::AddressAlreadyInUse) { throw }
    $rejected = $true
  }
  if (-not $rejected) { throw 'Occupied port was not refused.' }
  if ($env:REGENTE_GIT_SOURCE -ne 'https://example.invalid/must-not-clone') { throw 'Caller environment was not restored after failure.' }
  $client = [Net.Sockets.TcpClient]::new()
  try { $client.Connect('127.0.0.1', $portNumber) } finally { $client.Dispose() }
} finally {
  $listener.Stop()
  $env:REGENTE_GIT_SOURCE = $before
}
Write-Host 'PASS: unsafe modes rejected; occupied listener preserved; environment restored.'
