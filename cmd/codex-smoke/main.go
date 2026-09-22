// codex-smoke exercises the production adapter without creating conversations,
// refreshing credentials, invoking models, or launching Codex Desktop.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := codex.Start(ctx, codex.Options{Binary: os.Getenv("CODEX_BIN"), Socket: os.Getenv("CAELIS_CODEX_SOCKET")})
	if err != nil {
		return err
	}
	defer client.Close()
	status, err := client.ReadAuthStatus(ctx)
	if err != nil {
		return err
	}
	client.Close() // An owned child is reaped; an existing server is only disconnected.
	transport := "stdio"
	if client.UsesSharedServer() {
		transport = "unix-websocket"
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		TestedCodex         string           `json:"testedCodex"`
		Transport           string           `json:"transport"`
		Initialize          string           `json:"initialize"`
		Auth                codex.AuthStatus `json:"auth"`
		OwnedProcessClosed  bool             `json:"ownedProcessClosed"`
		ConversationCreated bool             `json:"conversationCreated"`
		ModelRequestSent    bool             `json:"modelRequestSent"`
	}{codex.TestedVersion, transport, "passed", status, !client.UsesSharedServer(), false, false})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
