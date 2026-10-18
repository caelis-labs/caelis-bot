. "$PSScriptRoot/windows-env.ps1"
$taskNSISPin = Get-Content -Raw -LiteralPath (Join-Path $taskBotRoot 'resources/windows/nsis.json') | ConvertFrom-Json
$taskNSISRoot = Join-Path $taskBotRoot ".cache/tools/nsis-$($taskNSISPin.version)-bundle-$($taskNSISPin.bundle)"
$taskNSISArchive = "$taskNSISRoot.tar.gz"
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $taskNSISRoot) | Out-Null
if (-not (Test-Path -LiteralPath $taskNSISArchive)) {
  Invoke-BotCommand node (Join-Path $PSScriptRoot 'release-network.mjs') curl.exe --fail --silent --show-error --location $taskNSISPin.url --output "$taskNSISArchive.download"
  if ((Get-FileHash -Algorithm SHA256 -LiteralPath "$taskNSISArchive.download").Hash.ToLowerInvariant() -ne $taskNSISPin.sha256) { throw 'NSIS archive checksum mismatch' }
  Move-Item -LiteralPath "$taskNSISArchive.download" -Destination $taskNSISArchive
}
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $taskNSISArchive).Hash.ToLowerInvariant() -ne $taskNSISPin.sha256) { throw 'Cached NSIS checksum mismatch' }
# Re-extract the verified archive: a cached compiler alone is not provenance.
New-Item -ItemType Directory -Force -Path $taskNSISRoot | Out-Null
Invoke-BotCommand tar -xf $taskNSISArchive -C $taskNSISRoot
$taskNSISDirectory = Join-Path $taskNSISRoot 'nsis-bundle/windows'
$taskMakeNSIS = Join-Path $taskNSISDirectory 'makensis.exe'
