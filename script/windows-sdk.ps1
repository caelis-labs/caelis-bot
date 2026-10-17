. "$PSScriptRoot/windows-env.ps1"
$pin = Get-Content -Raw -LiteralPath (Join-Path $taskBotRoot 'resources/windows/sdk-buildtools.json') | ConvertFrom-Json
$cache = Join-Path $taskBotRoot '.cache/tools/windows-sdk-buildtools'
$archive = "$cache.nupkg"
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $cache) | Out-Null
if (-not (Test-Path -LiteralPath $archive)) {
  $id = $pin.package.ToLowerInvariant()
  Invoke-WebRequest -Uri "https://api.nuget.org/v3-flatcontainer/$id/$($pin.version)/$id.$($pin.version).nupkg" -OutFile "$archive.partial"
  if ((Get-FileHash -LiteralPath "$archive.partial" -Algorithm SHA256).Hash.ToLowerInvariant() -ne $pin.sha256) { throw 'Windows SDK archive checksum mismatch' }
  Move-Item -LiteralPath "$archive.partial" -Destination $archive
}
if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $pin.sha256) { throw 'Windows SDK archive checksum mismatch' }
if (-not (Test-Path -LiteralPath $cache)) { Expand-Archive -LiteralPath $archive -DestinationPath $cache }
$bin = Join-Path $cache $pin.bin
if ((Get-AuthenticodeSignature -LiteralPath (Join-Path $bin 'makeappx.exe')).Status -ne 'Valid') { throw 'Windows SDK tool signature rejected' }
$env:PATH = "$bin;$env:PATH"
