
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/release-verification.ps1"
$Repo='Dr0nj/regente';$Version='latest'
$dist=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../dist'))
$env:REGENTE_MANIFEST=Join-Path $dist 'release-manifest.json'
$env:REGENTE_ATTESTATION=Join-Path $dist 'release-manifest.sigstore.json'
$env:REGENTE_RELEASE_ASSET=Join-Path $dist 'regente-agent_windows_amd64.exe'
$target=Join-Path $env:RUNNER_TEMP ('verified-'+[guid]::NewGuid()+'.exe')
Get-VerifiedRelease 'regente-agent_windows_amd64.exe' $target
$bytes=[IO.File]::ReadAllBytes($target)
$bytes[0]=$bytes[0] -bxor 1
$bad=Join-Path $env:RUNNER_TEMP ('tampered-'+[guid]::NewGuid()+'.exe')
[IO.File]::WriteAllBytes($bad,$bytes)
$env:REGENTE_RELEASE_ASSET=$bad
$rejected=$false
try { Get-VerifiedRelease 'regente-agent_windows_amd64.exe' $target }
catch {
  if ($_.Exception.Message -notmatch 'Release payload integrity failed') { throw }
  $rejected=$true
}
if (-not $rejected) { throw 'Windows verifier accepted modified payload' }
Remove-Item -LiteralPath $target,$bad -Force
Remove-Item Env:REGENTE_MANIFEST,Env:REGENTE_ATTESTATION,Env:REGENTE_RELEASE_ASSET
Write-Output 'I15 WINDOWS REAL SIGNATURE + MODIFIED PAYLOAD REFUSED'
