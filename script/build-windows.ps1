param([string]$OutputDirectory='',[ValidateSet('development','preview')][string]$Channel='development',[ValidateRange(1,65535)][int]$PreviewRevision=1)
. "$PSScriptRoot/windows-env.ps1"
# Native and installer qualification are separate from compiling either channel.
Invoke-BotCommand node script/asset-pack.mjs verify
Invoke-BotCommand npm.cmd run build
$taskVersionInfo = (& node script/windows-version.mjs $Channel $PreviewRevision | ConvertFrom-Json)
if ($LASTEXITCODE -ne 0) { throw 'Invalid source version' }
$taskVersion=$taskVersionInfo.version
$taskSource = & git rev-parse HEAD
if ($LASTEXITCODE -ne 0) { throw 'Cannot resolve source commit' }
$taskDirty = [bool](& git status --porcelain)
if($env:BOT_RELEASE_TAG) {
  $taskTagSource=& git rev-parse "$($env:BOT_RELEASE_TAG)^{commit}"
  if($LASTEXITCODE -ne 0 -or $taskDirty -or $taskTagSource -ne $taskSource){throw 'Tagged builds require a clean exact tagged checkout'}
}
$taskDestination = if($OutputDirectory) {[IO.Path]::GetFullPath($OutputDirectory)} else {Join-Path $taskBotRoot "dist/windows-$Channel"}
$taskDistPrefix = [IO.Path]::GetFullPath((Join-Path $taskBotRoot 'dist'))+[IO.Path]::DirectorySeparatorChar
if(-not $taskDestination.StartsWith($taskDistPrefix,[StringComparison]::OrdinalIgnoreCase)){throw 'Build output must stay inside workspace dist'}
if($Channel -eq 'preview' -and (Test-Path -LiteralPath $taskDestination)){throw 'Preview output must be fresh; choose a new OutputDirectory to avoid packaging stale files'}
New-Item -ItemType Directory -Force -Path $taskDestination | Out-Null
$taskResource = Join-Path $taskBotRoot 'rsrc_windows_amd64.syso'
if(Test-Path -LiteralPath $taskResource) {throw 'Existing Windows resource object; preserve before rebuilding'}
$taskOldCGO = $env:CGO_ENABLED
try {
  $env:CGO_ENABLED='0'
  Invoke-BotCommand go run ./cmd/windows-package-assets internal/desktop/assets/app-icon.png (Join-Path $taskDestination 'Resources/Windows')
  Invoke-BotCommand go run "github.com/wailsapp/wails/v3/cmd/wails3@$($taskBotPins.wails)" generate syso -arch amd64 -manifest resources/windows/app.manifest -icon (Join-Path $taskDestination 'Resources/Windows/app.ico') -out $taskResource
  Invoke-BotCommand go build -buildvcs=false -trimpath -tags production -ldflags "-H windowsgui -X github.com/caelis-labs/caelis-bot/internal/updates.Version=$taskVersion -X github.com/caelis-labs/caelis-bot/internal/desktop.buildChannel=$($taskVersionInfo.identity) -X github.com/caelis-labs/caelis-bot/internal/desktop.windowsReleaseUpdates=$($taskVersionInfo.releaseUpdates.ToString().ToLowerInvariant()) -X github.com/caelis-labs/caelis-bot/internal/caelisruntime.windowsBundled=1" -o (Join-Path $taskDestination 'caelis-bot.exe') .
  Invoke-BotCommand go build -buildvcs=false -trimpath -o (Join-Path $taskDestination 'caelis-terminal.exe') ./cmd/caelis-terminal
} finally {
  $env:CGO_ENABLED=$taskOldCGO
  if(Test-Path -LiteralPath $taskResource) {Remove-Item -LiteralPath $taskResource -Force}
}
Invoke-BotCommand python script/caelis-runtime.py stage-windows $taskDestination
& "$PSScriptRoot/stage-desktop-world-windows.ps1" -Destination (Join-Path $taskDestination 'Plugins/desktop-world')
foreach ($taskName in @('LICENSE','ASSET-LICENSE.md')) {
  Copy-Item -LiteralPath (Join-Path $taskBotRoot $taskName) -Destination (Join-Path $taskDestination 'Resources')
}
Copy-Item -LiteralPath protocol/caelis/LICENSE -Destination (Join-Path $taskDestination 'Resources/Caelis-Protocol-LICENSE')
Copy-Item -LiteralPath licenses/openclaw-weixin-LICENSE,licenses/go-qrcode-LICENSE,resources/character-pack.json -Destination (Join-Path $taskDestination 'Resources')
$taskHash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $taskDestination 'caelis-bot.exe')).Hash.ToLowerInvariant()
@{schema=1;version=$taskVersion;source=$taskSource;dirty=$taskDirty;platform='windows-amd64';channel=$Channel;identity=$taskVersionInfo.identity;releaseUpdates=$taskVersionInfo.releaseUpdates;sha256=$taskHash;qualified=$false} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $taskDestination 'build.json') -Encoding utf8NoBOM
Write-Output "Built $taskDestination; native and release acceptance are separate"
