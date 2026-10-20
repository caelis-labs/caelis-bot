package managedupdate

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
)

// A persistent release key must be reviewed and pinned before Linux publication.
// No generated or QA key is a production default.
//
//go:embed release-key.txt
var releaseKey string

type Trust struct {
	Key  ed25519.PublicKey
	Base string
	Feed string
	QA   bool
}

func ProductionTrust() Trust {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(releaseKey))
	return Trust{Key: key, Base: "https://github.com/caelis-labs/caelis-bot/releases/download",
		Feed: "https://api.github.com/repos/caelis-labs/caelis-bot/releases?per_page=100"}
}

// Test trust is only accepted by a separately tagged QA launcher, never a
// production binary, environment variable, HTTP request or ordinary config.
func ReadQATrust(file string) (Trust, error) {
	if !qaBuild {
		return Trust{}, problem("test_trust_disabled")
	}
	if err := privateFile(file); err != nil {
		return Trust{}, err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return Trust{}, err
	}
	var config struct {
		PublicKey string `json:"publicKey"`
		Origin    string `json:"origin"`
	}
	if json.Unmarshal(data, &config) != nil {
		return Trust{}, problem("test_trust_invalid")
	}
	key, err := base64.StdEncoding.DecodeString(config.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize || !qaOrigin(config.Origin) {
		return Trust{}, problem("test_trust_invalid")
	}
	return Trust{Key: key, Base: config.Origin + "/releases/download", Feed: config.Origin + "/releases", QA: true}, nil
}
