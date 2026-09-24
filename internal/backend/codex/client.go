package codex

import (
	"context"
	"encoding/json"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

// TestedVersion pins schema generation and reproducible regression evidence.
// It is not a runtime allowlist or a negotiated App Server protocol version.
const TestedVersion = "0.153.4"

type Client struct {
	rpc  *transport
	home string // Native handshake home, for attaching the user's TUI to this server.
}
type Options struct {
	Diagnostics *diagnosticlog.Logger
	Binary      string
	Socket      string
	Directory   string
	CLIOnly     bool // Probe a selected executable without falling back to another source.
	Attachable  bool // Owned session runtime exposes a private local Unix endpoint.
	// Session clients opt in for background-terminal cleanup and native requests.
	Experimental   bool
	HandleRequests bool
}

// Start prefers a listening standard local endpoint, then the user's CLI.
// ctx bounds startup, not client lifetime. Close never stops a shared server.
func Start(ctx context.Context, opts Options) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !opts.CLIOnly {
		probe, done := context.WithTimeout(ctx, 3*time.Second)
		conn, err := connectExisting(probe, opts.Socket)
		if err == nil {
			client, err := initializeClient(probe, conn, nil, opts)
			if err == nil {
				done()
				return client, nil
			}
		}
		done()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	conn, stop, err := startProcess(ctx, opts)
	if err != nil {
		return nil, err
	}
	return initializeClient(ctx, conn, stop, opts)
}
func initializeClient(ctx context.Context, conn connection, stop func(), opts Options) (*Client, error) {
	c := &Client{rpc: newTransportLogged(conn, stop, opts.HandleRequests, opts.Diagnostics)}
	params := map[string]any{"clientInfo": map[string]any{"name": "caelis_bot", "title": "Caelis Bot", "version": "0.0.1"},
		"capabilities": map[string]any{"experimentalApi": opts.Experimental, "requestAttestation": false}}
	result, err := c.rpc.call(ctx, "initialize", params)
	if err == nil {
		// App Server does not negotiate a protocolVersion. Accept additive fields
		// and older responses without unused home/platform metadata. Required
		// account/thread/turn/approval semantics are checked when consumed; this
		// handshake alone is not a claim that every experimental API is supported.
		var response struct {
			UserAgent string          `json:"userAgent"`
			CodexHome json.RawMessage `json:"codexHome"`
		}
		if json.Unmarshal(result, &response) != nil || response.UserAgent == "" {
			err = ErrProtocol
		}
		_ = json.Unmarshal(response.CodexHome, &c.home)
	}
	if err == nil {
		_, err = c.rpc.send(ctx, wireMessage{Method: "initialized"})
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *Client) Close()                             { c.rpc.close() }
func (c *Client) Notifications() <-chan Notification { return c.rpc.events }
func (c *Client) Done() <-chan struct{}              { return c.rpc.done }
func (c *Client) UsesSharedServer() bool             { _, shared := c.rpc.conn.(*socketConnection); return shared }

// Err reports the terminal connection cause after Done closes, or nil while open.
func (c *Client) Err() error { return c.rpc.cause() }

// AuthStatus deliberately omits email, account identifiers, credentials and paths.
// An account record is not proof of token validity or permission to execute work.
type AuthStatus struct {
	AccountPresent     bool   `json:"accountPresent"`
	AccountType        string `json:"accountType"`
	RequiresOpenAIAuth bool   `json:"requiresOpenaiAuth"`
}

func (c *Client) ReadAuthStatus(ctx context.Context) (AuthStatus, error) {
	b, err := c.rpc.call(ctx, "account/read", map[string]bool{"refreshToken": false})
	if err != nil {
		return AuthStatus{}, err
	}
	return projectAuth(b)
}
func projectAuth(b []byte) (AuthStatus, error) {
	var response struct {
		Account  json.RawMessage `json:"account"`
		Requires *bool           `json:"requiresOpenaiAuth"`
	}
	if json.Unmarshal(b, &response) != nil || response.Requires == nil {
		return AuthStatus{}, ErrProtocol
	}
	status := AuthStatus{RequiresOpenAIAuth: *response.Requires}
	// The pinned native JSON schema permits an omitted optional account.
	if len(response.Account) == 0 || string(response.Account) == "null" {
		return status, nil
	}
	var account struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(response.Account, &account) != nil {
		return AuthStatus{}, ErrProtocol
	}
	switch account.Type {
	case "apiKey", "chatgpt", "amazonBedrock":
		status.AccountPresent = true
		status.AccountType = account.Type
	default:
		return AuthStatus{}, ErrProtocol
	}
	return status, nil
}

func (c *Client) captureTools() {
	if owner, ok := c.rpc.conn.(interface{ captureTools() }); ok {
		owner.captureTools()
	}
}
func (c *Client) toolCleanupError() error {
	if owner, ok := c.rpc.conn.(interface{ toolCleanupError() error }); ok {
		return owner.toolCleanupError()
	}
	return nil
}
