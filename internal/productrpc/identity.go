package productrpc

import (
	"crypto/sha256"
	"encoding/hex"
)

// ProfileBotID is the existing product projection of the persistent Bot identity.
// It creates no new identity and never transfers a native runtime session.
func ProfileBotID(rawID string) string {
	h := sha256.Sum256([]byte("caelis-product-bot\x00" + rawID))
	return "bot-" + hex.EncodeToString(h[:])
}
