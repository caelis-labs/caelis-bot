# Issue #75 attachment input evidence

## Native extraction and draft lifecycle

`bash script/attachment-clipboard-native-test.sh` uses a uniquely named macOS
pasteboard. It passed ordinary text, two file URLs, a legacy Finder file list,
file URLs plus an image representation in one operation, and image bytes encoded
as PNG. It does not
read or change the global user clipboard. `internal/desktop/attachments_test.go`
covers opaque metadata, atomic invalid batches, duplicate paths, restart
recovery, thumbnail access, accepted/removable byte cleanup, and orphan cleanup.

## Native WebKit presentation

The eight screenshots below were taken from `script/build_and_run.sh --chat-preview`
with `BOT_PREVIEW_FIXTURE=attachments` and automatic WKWebView capture. The
fixture mounts the production History or Panel component and injects synthetic
draft metadata and a thumbnail. It does not connect to a Bot Runtime, send a
message, or exercise physical Command+V or Finder drag input.

| Surface | Chinese light | Chinese dark | English light | English dark |
| --- | --- | --- | --- | --- |
| History | [image](attachments-history-zh-CN-light.png) | [image](attachments-history-zh-CN-dark.png) | [image](attachments-history-en-light.png) | [image](attachments-history-en-dark.png) |
| Quick panel | [image](attachments-panel-zh-CN-light.png) | [image](attachments-panel-zh-CN-dark.png) | [image](attachments-panel-en-light.png) | [image](attachments-panel-en-dark.png) |

These captures verify both layouts, file details, thumbnail rendering, remove
controls, language switch, color scheme, and enabled send action for the fixture
state. Real WebKit clipboard event representations, user focus, Finder drag/drop,
and a real backend send still need independent isolated end-to-end acceptance.
