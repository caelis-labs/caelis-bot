$ErrorActionPreference = 'Stop'
$taskBotRoot = Split-Path -Parent $PSScriptRoot
$taskBotGitRoot = $taskBotRoot.Replace('\','/')
Set-Location -LiteralPath $taskBotRoot
$env:GOWORK = 'off'
$env:GOAMD64 = 'v1'
# Permit this known workspace for child validation tools without changing the
# user's global Git configuration or trusting other directories.
$taskGitCount = 0
if ($env:GIT_CONFIG_COUNT) { $taskGitCount = [int]$env:GIT_CONFIG_COUNT }
[Environment]::SetEnvironmentVariable("GIT_CONFIG_KEY_$taskGitCount", 'safe.directory', 'Process')
[Environment]::SetEnvironmentVariable("GIT_CONFIG_VALUE_$taskGitCount", $taskBotGitRoot, 'Process')
$env:GIT_CONFIG_COUNT = [string]($taskGitCount + 1)
$taskNodeVersion = (Get-Content -Raw -LiteralPath (Join-Path $taskBotRoot '.node-version')).Trim()
$taskNode = Join-Path $taskBotRoot ".cache/tools/node-v$taskNodeVersion-win-x64"
if (Test-Path -LiteralPath (Join-Path $taskNode 'node.exe')) { $env:PATH = "$taskNode;$env:PATH" }
if ((& node --version) -ne "v$taskNodeVersion") { throw "Use the pinned Node $taskNodeVersion" }
$taskModule = (& go mod edit -json | ConvertFrom-Json)
if ($LASTEXITCODE -ne 0) { throw 'Cannot read Go module versions' }
$taskGoVersion = $taskModule.Go
if (-not $taskGoVersion -or (& go version) -notmatch "go$([regex]::Escape($taskGoVersion)) windows/amd64") { throw 'Use pinned Go on Windows x64' }
$taskWailsModule = @($taskModule.Require | Where-Object { $_.Path -eq 'github.com/wailsapp/wails/v3' })
if ($taskWailsModule.Count -ne 1 -or -not $taskWailsModule[0].Version) { throw 'go.mod must pin the Wails application library' }
$taskWailsVersion = $taskWailsModule[0].Version
function Invoke-BotCommand {
  $taskCommand = $args[0]
  $taskArguments = @($args | Select-Object -Skip 1)
  & $taskCommand @taskArguments
  if ($LASTEXITCODE -ne 0) { throw "$taskCommand failed ($LASTEXITCODE)" }
}
