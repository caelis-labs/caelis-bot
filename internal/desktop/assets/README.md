# Status icon

`status-icon.png` is the 64px transparent Caelis girl illustration, shared with
the application icon and chat avatar at the user's request (2026-09-21).
It replaces the earlier sibling-homepage favicon. AppKit presents it as a
full-color status image up to 24pt, with a 2pt allowance for shorter menu bars,
not a monochrome template. This compensates for the character's transparent
breathing room without clipping its outline.

Source and reproducible size conversion remain in the private asset repository;
see [character assets](../../../docs/character-assets.md) for delivery and licensing.
`app-icon.png` supplies the matching Wails application/dialog icon.

The copy is embedded at build time. Builds do not import or read the sibling repo.
