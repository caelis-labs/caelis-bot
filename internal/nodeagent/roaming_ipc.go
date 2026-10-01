package nodeagent

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// RoamingIPCDirectory separates sockets from the durable deployment slot for
// the fixed HOME namespace used by native SSH enrollment. Legacy/custom native
// enrollments keep their existing slot and must pass the usual length checks.
func RoamingIPCDirectory(directory, operationID string) string {
	sum := sha256.Sum256([]byte(operationID))
	key := hex.EncodeToString(sum[:8])
	home := directory
	for range 4 {
		home = filepath.Dir(home)
	}
	if directory == filepath.Join(home, ".local", "share", "caelis-bot", "node-agent") {
		return filepath.Join(home, ".caelis-bot-joins", "r-"+key)
	}
	return filepath.Join(directory, "roaming-"+key)
}
