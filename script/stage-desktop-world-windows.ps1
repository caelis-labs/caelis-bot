param(
  [Parameter(Mandatory=$true)][string]$Destination
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$pin = Get-Content -Raw -LiteralPath (Join-Path $root 'resources/desktop-world/release.json') | ConvertFrom-Json
$target = $pin.targets.'windows-amd64'
if ($null -eq $target -or $target.artifact -ne 'dtw-windows-amd64.exe' -or $target.binary -ne 'bin/dtw.exe') {
  throw 'Unexpected Desktop World Windows target'
}
$cache = Join-Path $root ".cache/desktop-world-runtime/$($pin.version)"
New-Item -ItemType Directory -Force -Path $cache | Out-Null
function Download-Pinned([string]$Name, [string]$Expected) {
  $path = Join-Path $cache $Name
  if (-not (Test-Path -LiteralPath $path)) {
    $partial = "$path.partial"
    Invoke-WebRequest -Uri "https://github.com/caelis-labs/desktop-world/releases/download/$($pin.version)/$Name" -OutFile $partial
    Move-Item -LiteralPath $partial -Destination $path -Force
  }
  if ((Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant() -ne $Expected) {
    throw "Desktop World checksum mismatch: $Name"
  }
}
Download-Pinned $target.artifact $target.sha256
Download-Pinned 'BUILD-windows-amd64.json' $target.build_sha256
# Use the same provenance, offline exporter and complete-package verification on
# both platforms. Python is build tooling only; it is never shipped with Bot.
$plugins = Split-Path -Parent ([System.IO.Path]::GetFullPath($Destination))
if ((Split-Path -Leaf $Destination) -ne 'desktop-world') { throw 'Expected desktop-world destination' }
& python (Join-Path $PSScriptRoot 'stage-desktop-world-plugin.py') stage windows-amd64 $plugins --cache $cache
if ($LASTEXITCODE -ne 0) { throw 'Desktop World export or verification failed' }
& python (Join-Path $PSScriptRoot 'stage-desktop-world-plugin.py') inventory windows-amd64 $plugins
if ($LASTEXITCODE -ne 0) { throw 'Desktop World inventory generation failed' }
