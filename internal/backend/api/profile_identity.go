package api

import (
	"crypto/sha256"
	"encoding/hex"
)

// ProfileBotID projects the persistent native Bot ID for private product pairing.
func ProfileBotID(rawID string) string {
	h := sha256.Sum256([]byte("caelis-product-bot\x00" + rawID))
	return "bot-" + hex.EncodeToString(h[:])
}
