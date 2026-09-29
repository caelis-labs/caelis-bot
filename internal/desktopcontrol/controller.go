package desktopcontrol

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Controller owns exactly one private child. A new observation may open a new
// runtime after failure; an input never does. Native handles cannot survive it.
type Controller struct {
	mu           sync.Mutex
	node, script string
	driver       *Driver
	closed       bool
	focus        WindowFocuser
}

func Bundled(focus ...WindowFocuser) *Controller {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	root := filepath.Join(filepath.Dir(executable), "..", "Resources", "ComputerUse")
	node, script := filepath.Join(root, "node"), filepath.Join(root, "host.mjs")
	for _, path := range []string{node, script} {
		if info, e := os.Stat(path); e != nil || !info.Mode().IsRegular() {
			return nil
		}
	}
	c := &Controller{node: node, script: script}
	if len(focus) > 0 {
		c.focus = focus[0]
	}
	return c
}
func (c *Controller) Definitions() []api.ToolDefinition { return SemanticDefinitions() }
func (c *Controller) CallTool(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Err() != nil || c.closed {
		return driverError("desktop controller unavailable or cancelled")
	}
	if c.driver == nil {
		if name != "bot_desktop_observe" {
			return driverError("observe the desktop before performing actions")
		}
		d, err := StartDriver(c.node, c.script, c.focus)
		if err != nil {
			return driverError("desktop helper unavailable; check the application installation")
		}
		c.driver = d
	}
	result := c.driver.CallTool(ctx, name, args)
	c.driver.mu.Lock()
	closed := c.driver.closed
	c.driver.mu.Unlock()
	if closed {
		c.driver = nil
	}
	return result
}
func (c *Controller) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.driver != nil {
		c.driver.Close()
		c.driver = nil
	}
}
