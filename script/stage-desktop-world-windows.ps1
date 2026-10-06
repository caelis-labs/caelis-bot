param(
  [Parameter(Mandatory=$true)][string]$Destination,
  [string]$Archive = ''
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$pin = Get-Content -Raw -LiteralPath (Join-Path $root 'resources/desktop-world/release.json') | ConvertFrom-Json
$target = $pin.targets.'windows-amd64'
if ($pin.version -ne 'v0.1.0-rc.3' -or $pin.revision -ne '43172bb1f3dd26b90a88c346bc9978cbabab5785') { throw 'Unexpected Desktop World version' }
if ($target.archive -ne "desktop-world-$($pin.version)-windows-amd64.zip" -or $target.binary -ne 'bin/dtw.exe') { throw 'Unexpected Desktop World target' }
$cache = Join-Path $root '.cache/desktop-world-runtime'
New-Item -ItemType Directory -Force -Path $cache | Out-Null
if (-not $Archive) {
  $Archive = Join-Path $cache $target.archive
  if (-not (Test-Path -LiteralPath $Archive)) {
    $partial = "$Archive.partial"
    Invoke-WebRequest -Uri "https://github.com/caelis-labs/desktop-world/releases/download/$($pin.version)/$($target.archive)" -OutFile $partial
    Move-Item -LiteralPath $partial -Destination $Archive -Force
  }
}
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $Archive).Hash.ToLowerInvariant() -ne $target.sha256) { throw 'Desktop World archive checksum mismatch' }
$stage = Join-Path $cache ('.stage-' + [guid]::NewGuid().ToString('N'))
try {
  Expand-Archive -LiteralPath $Archive -DestinationPath $stage
  $source = Join-Path $stage "desktop-world-$($pin.version)-windows-amd64"
  & node (Join-Path $root 'script/verify-desktop-world-manifest.mjs') $source --target windows-amd64
  if ($LASTEXITCODE -ne 0) { throw 'Desktop World manifest rejected' }
  if (Test-Path -LiteralPath $Destination) { Remove-Item -LiteralPath $Destination -Recurse -Force }
  New-Item -ItemType Directory -Force -Path (Join-Path $Destination 'bin') | Out-Null
  Copy-Item -LiteralPath (Join-Path $source 'bin/dtw.exe') -Destination (Join-Path $Destination 'bin/dtw.exe')
  foreach ($name in @('manifest.json','LICENSE','NOTICE','THIRD_PARTY_NOTICES.md')) {
    Copy-Item -LiteralPath (Join-Path $source $name) -Destination (Join-Path $Destination $name)
  }
  Copy-Item -LiteralPath (Join-Path $source 'source') -Destination (Join-Path $Destination 'source') -Recurse
  & node (Join-Path $root 'script/verify-desktop-world-manifest.mjs') $Destination --target windows-amd64 --bundled
  if ($LASTEXITCODE -ne 0) { throw 'Staged Desktop World payload rejected' }
} finally {
  Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
}
