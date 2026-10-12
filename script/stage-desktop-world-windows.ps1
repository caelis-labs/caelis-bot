param(
  [Parameter(Mandatory=$true)][string]$Destination,
  [string]$Archive = ''
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$pin = Get-Content -Raw -LiteralPath (Join-Path $root 'resources/desktop-world/release.json') | ConvertFrom-Json
$target = $pin.targets.'windows-amd64'
if ($null -eq $target) { throw 'Desktop World windows-amd64 target unsupported by release pin' }
if ($pin.version -ne 'v0.1.0-rc.4' -or $pin.cua_revision -ne 'ac967b9006aafa10870aadb5a645b038cba9721a') { throw 'Unexpected Desktop World version' }
if ($target.archive -ne "desktop-world-$($pin.version)-windows-amd64.zip" -or $target.binary -ne 'bin/dtw.exe') { throw 'Unexpected Desktop World target' }
$cache = Join-Path $root '.cache/desktop-world-runtime'
New-Item -ItemType Directory -Force -Path $cache | Out-Null
function Confirm-Hash([string]$Path, [string]$Expected) {
  if (-not (Test-Path -LiteralPath $Path) -or (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant() -ne $Expected) {
    throw "Desktop World checksum mismatch: $(Split-Path -Leaf $Path)"
  }
}
function Download-Pinned([string]$Name, [string]$Expected) {
  $path = Join-Path $cache $Name
  if (-not (Test-Path -LiteralPath $path)) {
    $partial = "$path.partial"
    Invoke-WebRequest -Uri "https://github.com/caelis-labs/desktop-world/releases/download/$($pin.version)/$Name" -OutFile $partial
    Move-Item -LiteralPath $partial -Destination $path -Force
  }
  Confirm-Hash $path $Expected
  return $path
}
if (-not $Archive) { $Archive = Download-Pinned $target.archive $target.sha256 }
Confirm-Hash $Archive $target.sha256
$buildPath = Download-Pinned 'BUILD-windows-amd64.json' $target.build_sha256
$acceptancePath = Download-Pinned 'ACCEPTANCE.json' $pin.acceptance_sha256
$null = Download-Pinned $pin.source.archive $pin.source.sha256
$build = Get-Content -Raw -LiteralPath $buildPath | ConvertFrom-Json
$acceptance = Get-Content -Raw -LiteralPath $acceptancePath | ConvertFrom-Json
if ($acceptance.release -ne $pin.version -or $acceptance.cua_revision -ne $pin.cua_revision -or
    $build.tag -ne $pin.version -or $build.platform -ne 'windows-amd64' -or $build.archive_sha256 -ne $target.sha256 -or
    $build.source_revision -ne $pin.revision -or $build.cua_revision -ne $pin.cua_revision -or
    $build.adapter_sha256 -ne $target.binary_sha256 -or $build.worker_sha256 -ne $target.worker_sha256 -or
    $build.license_inventory_sha256 -ne $target.license_inventory_sha256 -or $build.license_packages -lt 300 -or
    @($build.mpl_source_packages).Count -lt 6) {
  throw 'Desktop World BUILD provenance or licensing mismatch'
}
$stage = Join-Path $cache ('.stage-' + [guid]::NewGuid().ToString('N'))
try {
  Expand-Archive -LiteralPath $Archive -DestinationPath $stage
  $source = Join-Path $stage 'desktop-world'
  if (-not (Test-Path -LiteralPath (Join-Path $source 'plugin.json'))) { throw 'Desktop World portable package missing' }
  $manifest = Get-Content -Raw -LiteralPath (Join-Path $source 'plugin.json') | ConvertFrom-Json
  $mcp = Get-Content -Raw -LiteralPath (Join-Path $source 'mcp.json') | ConvertFrom-Json
  if ($manifest.name -ne 'desktop-world' -or $manifest.version -ne '0.1.0-rc.4' -or $mcp.mcpServers.'desktop-world'.command -ne './bin/dtw.exe') {
    throw 'Desktop World portable package contract changed'
  }
  Confirm-Hash (Join-Path $source 'bin/dtw.exe') $target.binary_sha256
  Confirm-Hash (Join-Path $source 'libexec/cua-driver.exe') $target.worker_sha256
  Confirm-Hash (Join-Path $source 'LICENSING/DEPENDENCIES.json') $target.license_inventory_sha256
  $checks = @{}
  foreach ($line in Get-Content -LiteralPath (Join-Path $source 'SHA256SUMS')) {
    if ($line -notmatch '^([a-f0-9]{64})  (?:\./)?(.+)$') { throw 'Invalid Desktop World package checksum list' }
    $name = $Matches[2]; if ($checks.ContainsKey($name)) { throw 'Duplicate Desktop World package checksum' }
    if ($name.StartsWith('/') -or $name.Contains('..') -or $name.Contains('\')) { throw 'Unsafe Desktop World package checksum path' }
    $checks[$name] = $Matches[1]
    Confirm-Hash (Join-Path $source $name) $Matches[1]
  }
  $files = @(Get-ChildItem -LiteralPath $source -Recurse -File)
  if ($files.Count -ne $checks.Count + 1) {
    throw 'Desktop World package or licensing inventory incomplete'
  }
  foreach ($required in @('plugin.json', 'mcp.json', 'bin/dtw.exe', 'libexec/cua-driver.exe',
      'skills/desktop-world/SKILL.md', 'skills/desktop-world/api.d.ts', 'skills/desktop-world/RECIPES.md',
      'LICENSING/DEPENDENCIES.json', 'LICENSING/MPL-SOURCES.tar.gz', 'LICENSING/THIRD-PARTY.md',
      'LICENSING/rust-runtime-licenses/OFL-1.1.txt',
      'LICENSING/licenses/cursor-overlay-0.34.0/assets/Inter-OFL.txt',
      'CUA-MIT-LICENSE.txt', 'LICENSE', 'NOTICE')) {
    if (-not $checks.ContainsKey($required)) { throw "Desktop World missing $required" }
  }
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Destination) | Out-Null
  if (Test-Path -LiteralPath $Destination) { Remove-Item -LiteralPath $Destination -Recurse -Force }
  Copy-Item -LiteralPath $source -Destination $Destination -Recurse
  $files = @{}
  foreach ($file in Get-ChildItem -LiteralPath $Destination -Recurse -File) {
    $relative = [System.IO.Path]::GetRelativePath($Destination, $file.FullName).Replace('\', '/')
    $files[$relative] = (Get-FileHash -Algorithm SHA256 -LiteralPath $file.FullName).Hash.ToLowerInvariant()
  }
  $entry = @{
    ID = 'desktop-world'; Title = 'Computer Use'; titleZh = '电脑操控'; Version = '0.1.0-rc.4'
    Description = 'Operate desktop apps through one native CUA exec tool'
    Source = "bundled:desktop-world/$($pin.version)"; builtin = $true
    UpstreamIntegrity = $target.sha256
    executables = @('bin/dtw.exe', 'libexec/cua-driver.exe')
    display = @{ publisher = 'Caelis Labs'
      skills = @{ 'desktop-world' = @{ nameEn = 'Computer Use'; nameZh = '电脑操控'; descriptionEn = 'Inspect and operate an authorized desktop app.'; descriptionZh = '检查并操作已授权的桌面应用。' } }
      servers = @{ 'desktop-world' = @{ nameEn = 'Computer Use'; nameZh = '电脑操控'; descriptionEn = 'One native CUA exec tool.'; descriptionZh = '单一原生 CUA exec 工具。' } }
    }
    Files = $files
  }
  $inventoryPath = Join-Path (Split-Path -Parent $Destination) 'desktop-world-inventory.json'
  $entry | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $inventoryPath -Encoding utf8NoBOM
  Write-Host "Verified complete Desktop World $($pin.version) windows-amd64: $($files.Count) files"
} finally {
  Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
}
