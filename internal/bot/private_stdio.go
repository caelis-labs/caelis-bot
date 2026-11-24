package bot

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const caelisMCPBridgeVersion = 3

type residentMCPConnection struct {
	Endpoint string               `json:"endpoint"`
	Token    string               `json:"token"`
	Catalog  []api.ToolDefinition `json:"catalog"`
}

func (b *Bridge) publishCaelisMCP(notebook string) (string, error) {
	if !filepath.IsAbs(notebook) {
		return "", errors.New("Caelis resident MCP requires the private Notebook directory")
	}
	dir := filepath.Join(notebook, ".bot-runtime")
	if err := localstate.EnsurePrivateDirectory(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "resident-mcp.json")
	if err := localstate.Write(path, residentMCPConnection{Endpoint: b.listener.Endpoint(), Token: b.caelisMCPToken, Catalog: b.caelisCatalog}); err != nil {
		return "", err
	}
	return path, nil
}

// RunStdioArgs uses the environment for Codex and the resident Notebook
// connection file for the Caelis stdio child.
func RunStdioArgs(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return RunStdio(in, out)
	}
	if len(args) != 1 || !filepath.IsAbs(args[0]) {
		return errors.New("invalid Bot resident MCP launch")
	}
	path := args[0]
	connection, err := readResidentMCPConnection(path)
	if err != nil {
		return err
	}
	catalog, err := json.Marshal(connection.Catalog)
	if err != nil {
		return err
	}
	env := map[string]string{"CAELIS_BOT_SERVICE_NAME": "caelis_tasks", "CAELIS_BOT_TOOL_CATALOG": string(catalog)}
	instance := rand.Text()
	return serveStdio(in, out, func(key string) string { return env[key] }, func(call toolRequest) toolResult {
		// Core may retain its stdio child while Bot restarts. Read the
		// latest connection before calls and preserve one identity on retry.
		connection, err := readResidentMCPConnection(path)
		if err != nil {
			return compactError("runtime_unavailable", err)
		}
		call.ID, _ = json.Marshal(instance + ":" + string(call.ID))
		call.Token, call.Version = connection.Token, caelisMCPBridgeVersion
		return forward(connection.Endpoint, call)
	})
}

func readResidentMCPConnection(path string) (residentMCPConnection, error) {
	var connection residentMCPConnection
	invalid := errors.New("Bot resident MCP connection is unavailable")
	prior, err := os.Lstat(path)
	if err != nil || !prior.Mode().IsRegular() || !localstate.RestrictedAccess(path, prior) {
		return connection, invalid
	}
	file, err := os.Open(path)
	if err != nil {
		return connection, invalid
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !os.SameFile(prior, current) || !localstate.RestrictedAccess(path, current) {
		return connection, invalid
	}
	err = json.NewDecoder(io.LimitReader(file, 128<<10)).Decode(&connection)
	if err != nil || connection.Endpoint == "" || connection.Token == "" || len(connection.Catalog) == 0 {
		return residentMCPConnection{}, invalid
	}
	return connection, nil
}
