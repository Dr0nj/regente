function Get-VerifiedRelease {
  param([string]$Asset,[string]$Destination)
  if ($Repo -cne 'Dr0nj/regente') { throw 'Untrusted release repository' }
  if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { throw 'Install a trusted GitHub CLI with attestation support first' }
  $temp = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString())
  New-Item -ItemType Directory -Path $temp | Out-Null
  try {
    $base = if ($Version -eq 'latest') { "https://github.com/$Repo/releases/latest/download" } else { "https://github.com/$Repo/releases/download/$Version" }
    $manifest = Join-Path $temp 'release-manifest.json'
    $signature = Join-Path $temp 'release-manifest.sigstore.json'
    Invoke-WebRequest "$base/release-manifest.json" -OutFile $manifest -UseBasicParsing
    Invoke-WebRequest "$base/release-manifest.sigstore.json" -OutFile $signature -UseBasicParsing
    $m = Get-Content -LiteralPath $manifest -Raw | ConvertFrom-Json
    if ($m.schema -ne 1 -or $m.repository -cne 'Dr0nj/regente' -or $m.workflow -cne '.github/workflows/release.yml' -or $m.version -cnotmatch '^v[0-9]+\.[0-9]+\.[0-9]+$' -or $m.sourceSha -cnotmatch '^[a-f0-9]{40}$') { throw 'Invalid release manifest' }
    if ($Version -ne 'latest' -and $Version -cne $m.version) { throw 'Requested version differs' }
    if ($m.sourceRef -cne 'refs/heads/main' -and $m.sourceRef -cne "refs/tags/$($m.version)") { throw 'Untrusted source ref' }
    & gh attestation verify $manifest --bundle $signature --repo Dr0nj/regente --signer-workflow Dr0nj/regente/.github/workflows/release.yml --source-digest $m.sourceSha --source-ref $m.sourceRef --deny-self-hosted-runners
    if ($LASTEXITCODE -ne 0) { throw 'Release identity verification failed' }
    Invoke-WebRequest "https://github.com/$Repo/releases/download/$($m.version)/$Asset" -OutFile $Destination -UseBasicParsing
    $a=$m.assets.PSObject.Properties[$Asset].Value
    if (-not $a -or (Get-Item -LiteralPath $Destination).Length -ne $a.bytes -or (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash.ToLowerInvariant() -cne $a.sha256) { throw 'Release payload integrity failed' }
  } finally {
    # Caminho absoluto criado com GUID, restrito ao diretório temporário da função.
    $resolved = [IO.Path]::GetFullPath($temp)
    $parent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    if (-not $resolved.StartsWith($parent,[StringComparison]::OrdinalIgnoreCase)) { throw 'Unsafe temporary path' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
  }
}
