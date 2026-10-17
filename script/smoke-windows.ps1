param([string]$CodexBinary='')
. "$PSScriptRoot/windows-env.ps1"
$taskPreviousCodex=$env:CODEX_BIN
try {
  if($CodexBinary){$env:CODEX_BIN=[IO.Path]::GetFullPath($CodexBinary)}
  elseif(-not $env:CODEX_BIN){$env:CODEX_BIN=(Get-Command codex.exe -ErrorAction Stop).Source}
  Invoke-BotCommand npm.cmd run smoke:codex
  Invoke-BotCommand npm.cmd run smoke:assets
} finally { $env:CODEX_BIN=$taskPreviousCodex }
