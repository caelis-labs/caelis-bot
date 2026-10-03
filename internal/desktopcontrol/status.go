package desktopcontrol

import (
	"context"
	"encoding/json"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// ReadRun never starts a helper, grants a turn or dispatches an operation. Only
// runs already observed by this controller may resolve to their original receipt.
func (c *Controller) ReadRun(ctx context.Context, run string) api.ToolResult {
	c.mu.Lock()
	requests := make(map[string]*request, len(c.requests))
	for id, r := range c.requests {
		requests[id] = r
	}
	c.mu.Unlock()
	for id, r := range requests {
		if r.op != "act" {
			continue
		}
		select {
		case <-r.done:
		default:
			continue
		}
		var body struct {
			RunID string `json:"run_id"`
		}
		if json.Unmarshal(r.reply.Result, &body) == nil && body.RunID == run && run != "" {
			return c.reconcile(ctx, id)
		}
	}
	return failure("receipt_unavailable", "Original run not known to this host; no replay.", "")
}
