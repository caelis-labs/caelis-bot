# Native desktop fixture

These historical AppKit/browser fixtures remain disposable UI targets for
desktop integration experiments. Start the AppKit fixture through:

```sh
bash script/build_and_run.sh --desktop-control-preview
```

Production Desktop World v0.1.0-rc.4 is the optional built-in plugin with one
MCP `exec` and a Skill; the old Go desktop controller and `bot_desktop_*` tools
have been removed. Follow `docs/development.md` for Bot build and native
acceptance. A fixture alone does not establish real-model, OS-permission or
released-app acceptance.
