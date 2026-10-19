. "$PSScriptRoot/windows-env.ps1"
$taskPreviousCGO=$env:CGO_ENABLED
try {
  $env:CGO_ENABLED='0'
  Invoke-BotCommand go run ./cmd/contract-gen --check
  Invoke-BotCommand node script/check-public-tree.mjs
  Invoke-BotCommand python script/test_caelis_runtime.py
  Invoke-BotCommand node script/check-caelis-protocol.mjs
  Invoke-BotCommand node script/asset-pack.mjs verify
  Invoke-BotCommand npm.cmd run check:i18n
  Invoke-BotCommand npm.cmd run build
  # macOS signing/notarization and AppKit tests remain in check.sh on macOS.
  # This list covers portable product contracts plus native Windows adapters.
  $taskNodeTests=@(
    'script/release-network.test.mjs','script/windows-publication.test.mjs','script/ci-scope.test.mjs','script/release-version.test.mjs','script/windows-version.test.mjs','script/windows-update-manifest.test.mjs','script/windows-task-order.test.mjs',
    'script/setup-stage.test.mjs','script/permission-actions.test.mjs','script/native-call.test.mjs','script/read-recovery.test.mjs','script/login-at-login.test.mjs',
    'script/task-preferences.test.mjs','script/runtime-settings.test.mjs','script/connections-loader.test.mjs','script/codex-runtime-panel.test.mjs',
    'script/desktop-world.test.mjs','script/content-pack.test.mjs','script/asset-pack.test.mjs',
    'script/chat-presentation.test.mjs','script/chat-observation.test.mjs','script/chat-time.test.mjs','script/connection-notice.test.mjs',
    'script/reduced-motion.test.mjs','script/streaming-text.test.mjs','script/markdown-chunks.test.mjs','script/capture-editor.test.mjs','script/pet-interaction.test.mjs'
  )
  Invoke-BotCommand node --test @taskNodeTests
  # Bound package concurrency on hosted Windows runners. The timeout applies to
  # an entire package (hundreds of native-process tests), not a request deadline.
  Invoke-BotCommand go test -p 2 -timeout 180s ./...
  Invoke-BotCommand git diff --check
} finally { $env:CGO_ENABLED=$taskPreviousCGO }
