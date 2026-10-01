package productrpc

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// These native client helpers use the closed settings wire, not remote method
// names or arbitrary Service dispatch. Mutations retain their original receipts.
func (c *Client) AddNode(ctx context.Context, id string, in api.NodeAddRequest) (Result, error) {
	in.OperationID = id
	return c.ManageNodes(ctx, id, NodeCommand{Action: "add-node", Add: &in})
}
func (c *Client) SaveWorkerNode(ctx context.Context, id string, in RegisteredWorkerSelection) (Result, error) {
	return c.ManageNodes(ctx, id, NodeCommand{Action: "save-worker-node", Worker: &in})
}
func (c *Client) ConnectWorkerTarget(ctx context.Context, id string, in RegisteredWorkerSelection) (Result, error) {
	return c.ManageNodes(ctx, id, NodeCommand{Action: "connect-worker-target", Worker: &in})
}
func (c *Client) SaveNotebookSyncSettings(ctx context.Context, id string, in backend.NotebookSyncSettings) (Result, error) {
	return c.ManageNodes(ctx, id, NodeCommand{Action: "save-notebook-settings", NotebookSettings: &in})
}
func (c *Client) SyncNotebook(ctx context.Context, id, node string) (Result, error) {
	return c.ManageNodes(ctx, id, NodeCommand{Action: "sync-notebook", NodeID: node})
}
func (c *Client) SwitchNotebookNode(ctx context.Context, id, node string) (Result, error) {
	return c.ManageNodes(ctx, id, NodeCommand{Action: "switch-notebook-node", NodeID: node})
}

func (c *Client) UpdateNodeHelper(ctx context.Context, id string, in api.NodeHelperUpdateRequest) (Result, error) {
	return c.ManageNodes(ctx, id, NodeCommand{Action: "update-node-helper", HelperUpdate: &in})
}
func (c *Client) SaveNodeRuntimeSettings(ctx context.Context, id string, in api.NodeRuntimeSettingsRequest) (Result, error) {
	return c.ManageNodes(ctx, id, NodeCommand{Action: "save-node-runtime-settings", RuntimeSettings: &in})
}
