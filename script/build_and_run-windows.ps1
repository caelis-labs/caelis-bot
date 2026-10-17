param([string]$DataDirectory='', [switch]$SkipBuild, [string]$ApplicationDirectory='')
. "$PSScriptRoot/windows-env.ps1"
if($ApplicationDirectory -and -not $SkipBuild) {throw 'Use SkipBuild when selecting an existing candidate bundle'}
if(-not $SkipBuild) { & "$PSScriptRoot/build-windows.ps1" }
$taskBundle = if($ApplicationDirectory){[IO.Path]::GetFullPath($ApplicationDirectory)}else{Join-Path $taskBotRoot 'dist/windows-development'}
$taskBinary = Join-Path $taskBundle 'caelis-bot.exe'
if(-not (Test-Path -LiteralPath $taskBinary)) {throw 'Build the native application first'}
$taskBuild=Get-Content -Raw -LiteralPath (Join-Path $taskBundle 'build.json') | ConvertFrom-Json
if((Get-FileHash -Algorithm SHA256 -LiteralPath $taskBinary).Hash.ToLowerInvariant() -ne $taskBuild.sha256) {throw 'Candidate executable differs from its build receipt'}
$taskOriginalData = $env:CAELIS_BOT_DATA_DIR
try {
  if($DataDirectory) { $env:CAELIS_BOT_DATA_DIR=[IO.Path]::GetFullPath($DataDirectory) }
  # Never kill a running Bot/Host to make a development restart succeed. The
  # native single-instance route recalls the existing installation when needed.
  $taskProcess=Start-Process -FilePath $taskBinary -WorkingDirectory $taskBundle -WindowStyle Hidden -PassThru
  Write-Output "Started Windows development instance PID $($taskProcess.Id)"
} finally { $env:CAELIS_BOT_DATA_DIR=$taskOriginalData }
