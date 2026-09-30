# Native desktop fixture

The AppKit fixture and local browser HTML remain disposable UI surfaces for
Desktop World acceptance. Start AppKit through:

```sh
bash script/build_and_run.sh --desktop-control-preview
```

Production tools now use `internal/desktopcontrol` and the pinned Desktop World
Go host/helper. Follow `docs/development.md` for the actual Bot build and
`internal/botskills/skills/caelis-bot-memory/references/desktop-observation.md`
for tool semantics. Fixture success alone does not qualify real application or
model usability. Use the fixture only with the current Desktop World tool path.
