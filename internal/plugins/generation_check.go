package plugins

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"
)

// generationCheck shares decoding between the timer and RPC gate, never a file
// read or a permission decision. Every call checks fresh committed bytes.
type generationCheck struct {
	mu         sync.Mutex
	path       string
	packageID  string
	generation string
	body       []byte
	enabled    bool
}

func (g *generationCheck) active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	body, err := os.ReadFile(g.path)
	if err != nil {
		return false
	}
	if bytes.Equal(body, g.body) {
		return g.enabled
	}
	var saved state
	if json.Unmarshal(body, &saved) != nil {
		return false
	}
	item, ok := saved.Installed[g.packageID]
	g.body = body
	g.enabled = ok && item.Enabled && item.Root == g.generation
	return g.enabled
}
