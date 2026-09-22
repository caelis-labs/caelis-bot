# Status icon

`status-icon.png` is the 64px transparent Caelis girl illustration, shared with
the application icon and chat avatar at the user's request (2026-09-21).
It replaces the earlier sibling-homepage favicon. AppKit presents it as a
full-color 18pt status image, not a monochrome template.

Source, transparent cutout and reproducible size conversion:
[`resources/brand/README.md`](../../../resources/brand/README.md).
`app-icon.png` supplies the matching Wails application/dialog icon.

The copy is embedded at build time. Builds do not import or read the sibling repo.
