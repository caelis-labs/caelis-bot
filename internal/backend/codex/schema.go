package codex

import (
	_ "embed"
	"encoding/json"
)

//go:embed schema/manifest.json
var schemaManifest []byte

// TestedVersion reports the embedded schema's reproducible regression baseline.
// It is not a runtime allowlist or a negotiated App Server protocol version.
var TestedVersion = func() string {
	var manifest struct {
		Codex string `json:"codex"`
	}
	if err := json.Unmarshal(schemaManifest, &manifest); err != nil || manifest.Codex == "" {
		panic("invalid embedded Codex schema manifest")
	}
	return manifest.Codex
}()
