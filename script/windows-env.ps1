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
$taskBotPins = Get-Content -Raw -LiteralPath (Join-Path $taskBotRoot 'toolchain.json') | ConvertFrom-Json
$taskNode = Join-Path $taskBotRoot ".cache/tools/node-v$($taskBotPins.node)-win-x64"
if (Test-Path -LiteralPath (Join-Path $taskNode 'node.exe')) { $env:PATH = "$taskNode;$env:PATH" }
if ((& node --version) -ne "v$($taskBotPins.node)") { throw "Use the pinned Node $($taskBotPins.node)" }
if ((& go version) -notmatch "go$([regex]::Escape($taskBotPins.go)) windows/amd64") { throw 'Use pinned Go on Windows x64' }
function Invoke-BotCommand {
  $taskCommand = $args[0]
  $taskArguments = @($args | Select-Object -Skip 1)
  & $taskCommand @taskArguments
  if ($LASTEXITCODE -ne 0) { throw "$taskCommand failed ($LASTEXITCODE)" }
}
