param(
  [switch]$SkipBuild,
  [switch]$ReplaceLocalDraft,
  [ValidateSet('development','preview')][string]$Channel='preview',
  [string]$ApplicationDirectory='',
  [ValidateRange(0,65535)][int]$PackageRevision=1
)
. "$PSScriptRoot/windows-nsis.ps1"
if (-not $SkipBuild) { & "$PSScriptRoot/build-windows.ps1" -Channel $Channel -OutputDirectory $ApplicationDirectory }
$app=if($ApplicationDirectory){[IO.Path]::GetFullPath($ApplicationDirectory)}else{Join-Path $taskBotRoot "dist/windows-$Channel"}
$build=Get-Content -Raw -LiteralPath (Join-Path $app 'build.json') | ConvertFrom-Json
if ($build.channel -ne $Channel -or $build.platform -ne 'windows-amd64') { throw 'Build identity mismatch' }
if ((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $app 'caelis-bot.exe')).Hash.ToLowerInvariant() -ne $build.sha256) { throw 'App changed after build' }
if ($env:BOT_RELEASE_TAG -and ($build.dirty -or $build.version -ne $env:BOT_RELEASE_TAG.Substring(1))) { throw 'Tagged packaging requires clean exact source' }
if ($env:BOT_RELEASE_TAG) {
  $tagSource=& git -c "safe.directory=$taskBotGitRoot" rev-parse "$($env:BOT_RELEASE_TAG)^{commit}"
  if ($LASTEXITCODE -ne 0 -or $tagSource -ne $build.source) {throw 'Build source does not match the immutable release tag'}
  $currentSource=& git -c "safe.directory=$taskBotGitRoot" rev-parse HEAD
  if ($LASTEXITCODE -ne 0 -or $currentSource -ne $tagSource -or (& git -c "safe.directory=$taskBotGitRoot" status --porcelain)) {throw 'Tagged packaging requires the clean tagged checkout'}
}
$name=if($build.identity -eq 'windows-preview') {'Caelis Bot Preview'} else {'Caelis Bot Dev'}
$identity=if($build.identity -eq 'windows-preview') {'dev.caelis.bot.preview'} else {'dev.caelis.bot.dev'}
$releaseDir=Join-Path $taskBotRoot 'dist/releases'
New-Item -ItemType Directory -Force -Path $releaseDir | Out-Null
$suffix=if($env:BOT_RELEASE_TAG) {"-$Channel"} else {"-$Channel-local"}
$package=Join-Path $releaseDir "Caelis-Bot-$($build.version)-windows-amd64$suffix-Setup.exe"
if (Test-Path -LiteralPath $package) {
  if ($env:BOT_RELEASE_TAG -or -not $ReplaceLocalDraft) { throw 'Package already exists; only local drafts can be replaced explicitly' }
  $resolved=[IO.Path]::GetFullPath($package)
  $allowed=[IO.Path]::GetFullPath($releaseDir)+[IO.Path]::DirectorySeparatorChar
  if (-not $resolved.StartsWith($allowed,[StringComparison]::OrdinalIgnoreCase) -or -not $resolved.EndsWith('-local-Setup.exe',[StringComparison]::OrdinalIgnoreCase)) { throw 'Local draft path rejected' }
  foreach($file in @($package,"$package.sha256","$package.build.json")) {if(Test-Path -LiteralPath $file) {Remove-Item -LiteralPath $file -Force}}
}
$work=Join-Path $taskBotRoot ('.cache/nsis-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
$previousNSIS=$env:NSISDIR
try {
  $env:NSISDIR=$taskNSISDirectory
  Copy-Item -LiteralPath (Join-Path $taskBotRoot 'resources/windows/installer.nsi') -Destination $work
  Invoke-BotCommand go run ./cmd/windows-package-assets internal/desktop/assets/app-icon.png (Join-Path $work 'Assets')
  $options=@{name=$name;id=$identity;version=$build.version;output=$package;icon=(Join-Path $work 'Assets/app.ico');nsisLicense=(Join-Path $taskNSISDirectory 'COPYING');revision=$PackageRevision} | ConvertTo-Json -Compress
  Invoke-BotCommand node script/windows-installer.mjs $app $work $options
  Invoke-BotCommand $taskMakeNSIS /INPUTCHARSET UTF8 /V2 (Join-Path $work 'installer.nsi')
  $signature=Get-AuthenticodeSignature -LiteralPath $package
  if ($signature.Status -ne 'NotSigned') { throw 'Unsigned preview policy requires an unsigned installer' }
  $hash=(Get-FileHash -Algorithm SHA256 -LiteralPath $package).Hash.ToLowerInvariant()
  "$hash  $([IO.Path]::GetFileName($package))" | Set-Content -LiteralPath "$package.sha256" -Encoding utf8NoBOM
  @{schema=1;version=$build.version;packageVersion="$($build.version.Split('-')[0]).$PackageRevision";source=$build.source;dirty=$build.dirty;channel=$Channel;platform='windows-amd64';format='nsis';sha256=$hash;signed=$false;signingPolicy='unsigned-preview';compiler=$taskNSISPin;qualified=$false} | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath "$package.build.json" -Encoding utf8NoBOM
  Write-Output "Packaged $package (unsigned; native/install acceptance is separate)"
} finally {
  $env:NSISDIR=$previousNSIS
  $resolved=[IO.Path]::GetFullPath($work)
  $allowed=[IO.Path]::GetFullPath((Join-Path $taskBotRoot '.cache'))+[IO.Path]::DirectorySeparatorChar
  if (-not $resolved.StartsWith($allowed,[StringComparison]::OrdinalIgnoreCase)) { throw 'Packaging cleanup path rejected' }
  if(Test-Path -LiteralPath $resolved) {Remove-Item -LiteralPath $resolved -Recurse -Force}
}
