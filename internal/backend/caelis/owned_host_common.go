package caelis

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"os"
	"sync"
)

// OwnedHostOptions are native target-side configuration, never Notebook data.
// Authentication remains in this explicitly designated node-owned store.
type OwnedHostOptions struct{ NodeID, Binary, Store, WatchdogHelper string }
type ownedHost struct {
	process  *codex.OwnedForeground
	settings api.RuntimeSettings
	instance string
	lock     *os.File
	once     sync.Once
	stopErr  error
}
