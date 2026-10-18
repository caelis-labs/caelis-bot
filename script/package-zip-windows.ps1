param([switch]$SkipBuild,[switch]$ReplaceLocalDraft,[ValidateSet('development','preview')][string]$Channel='preview',[string]$ApplicationDirectory='')
. "$PSScriptRoot/windows-env.ps1"
if(-not $SkipBuild){& "$PSScriptRoot/build-windows.ps1" -Channel $Channel -OutputDirectory $ApplicationDirectory}
$taskApp=if($ApplicationDirectory){[IO.Path]::GetFullPath($ApplicationDirectory)}else{Join-Path $taskBotRoot "dist/windows-$Channel"}
$taskBuild=Get-Content -Raw -LiteralPath (Join-Path $taskApp 'build.json')|ConvertFrom-Json
if($taskBuild.channel -ne $Channel -or $taskBuild.platform -ne 'windows-amd64' -or (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $taskApp 'caelis-bot.exe')).Hash.ToLowerInvariant() -ne $taskBuild.sha256){throw 'Build identity mismatch'}
if($env:BOT_RELEASE_TAG -and ($taskBuild.dirty -or $taskBuild.version -ne $env:BOT_RELEASE_TAG.Substring(1))){throw 'Tagged packaging requires clean exact source'}
if($env:BOT_RELEASE_TAG){$taskTagSource=& git -c "safe.directory=$taskBotGitRoot" rev-parse "$($env:BOT_RELEASE_TAG)^{commit}";if($LASTEXITCODE -ne 0 -or $taskTagSource -ne $taskBuild.source -or (& git -c "safe.directory=$taskBotGitRoot" rev-parse HEAD) -ne $taskTagSource -or (& git -c "safe.directory=$taskBotGitRoot" status --porcelain)){throw 'Tagged ZIP requires the clean immutable release source'}}
$taskReleaseDir=Join-Path $taskBotRoot 'dist/releases';New-Item -ItemType Directory -Force -Path $taskReleaseDir|Out-Null
$taskSuffix=if($env:BOT_RELEASE_TAG){"-$Channel"}else{"-$Channel-local"}
$taskPackage=Join-Path $taskReleaseDir "Caelis-Bot-$($taskBuild.version)-windows-amd64$taskSuffix.zip"
if(Test-Path -LiteralPath $taskPackage){if($env:BOT_RELEASE_TAG -or -not $ReplaceLocalDraft){throw 'Package exists; preserve immutable release artifacts'};if(-not $taskPackage.EndsWith('-local.zip') -or [IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($taskPackage)) -ne [IO.Path]::GetFullPath($taskReleaseDir)){throw 'Local draft path rejected'};foreach($taskFile in @($taskPackage,"$taskPackage.sha256","$taskPackage.build.json")){if(Test-Path -LiteralPath $taskFile){Remove-Item -LiteralPath $taskFile -Force}}}
$taskWork=Join-Path $taskBotRoot ('.cache/zip-'+[guid]::NewGuid().ToString('N'));New-Item -ItemType Directory -Path $taskWork|Out-Null
try{
 Copy-Item -Path (Join-Path $taskApp '*') -Destination $taskWork -Recurse
 $taskIdentity=if($taskBuild.identity -eq 'windows-preview'){'dev.caelis.bot.preview'}else{'dev.caelis.bot.dev'}
 Invoke-BotCommand node script/windows-portable.mjs $taskWork $taskIdentity $taskBuild.version $taskPackage
 $taskHash=(Get-FileHash -Algorithm SHA256 -LiteralPath $taskPackage).Hash.ToLowerInvariant()
 "$taskHash  $([IO.Path]::GetFileName($taskPackage))"|Set-Content -LiteralPath "$taskPackage.sha256" -Encoding utf8NoBOM
 @{schema=1;version=$taskBuild.version;source=$taskBuild.source;dirty=$taskBuild.dirty;channel=$Channel;platform='windows-amd64';format='zip';sha256=$taskHash;signed=$false;signingPolicy='unsigned-preview';qualified=$false}|ConvertTo-Json|Set-Content -LiteralPath "$taskPackage.build.json" -Encoding utf8NoBOM
 Write-Output "Packaged $taskPackage (native/update acceptance is separate)"
}finally{
 $taskResolved=[IO.Path]::GetFullPath($taskWork);$taskAllowed=[IO.Path]::GetFullPath((Join-Path $taskBotRoot '.cache'))+[IO.Path]::DirectorySeparatorChar
 if(-not $taskResolved.StartsWith($taskAllowed,[StringComparison]::OrdinalIgnoreCase)){throw 'ZIP cleanup path rejected'}
 Remove-Item -LiteralPath $taskResolved -Recurse -Force
}
