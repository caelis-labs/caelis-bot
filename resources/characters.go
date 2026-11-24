// Package resources exposes the same built-in character manifest used by the renderer.
package resources

import _ "embed"

//go:embed character-pack.json
var CharacterPack string
