package bot

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const caelisTaskBridgeVersion = 3

type taskMCPConnection struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

func caelisTaskTool(name string) bool { return name == "bot_delegate" || name == "bot_tasks" }

func (b *Bridge) publishCaelisTaskMCP(notebook string) (string, error) {
	if !filepath.IsAbs(notebook) {
		return "", errors.New("Caelis task MCP requires the private Notebook directory")
	}
	dir := filepath.Join(notebook, ".bot-runtime")
	if err := localstate.EnsurePrivateDirectory(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "tasks-mcp.json")
	if err := localstate.Write(path, taskMCPConnection{Endpoint: b.listener.Endpoint(), Token: b.caelisTaskToken}); err != nil {
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
		return errors.New("invalid Bot task MCP launch")
	}
	path := args[0]
	if _, err := readTaskMCPConnection(path); err != nil {
		return err
	}
	definitions := slices.DeleteFunc(toolDefinitions(), func(tool api.ToolDefinition) bool { return !caelisTaskTool(tool.Name) })
	catalog, _ := json.Marshal(definitions)
	env := map[string]string{"CAELIS_BOT_SERVICE_NAME": "caelis_tasks", "CAELIS_BOT_TOOL_CATALOG": string(catalog)}
	instance := rand.Text()
	return serveStdio(in, out, func(key string) string { return env[key] }, func(call toolRequest) toolResult {
		if !caelisTaskTool(call.Name) {
			return compactError("tool_unavailable", errors.New("This task service does not expose that tool."))
		}
		// Core may retain its stdio child while Bot restarts. Read the
		// latest connection before calls and preserve one identity on retry.
		connection, err := readTaskMCPConnection(path)
		if err != nil {
			return compactError("runtime_unavailable", err)
		}
		call.ID, _ = json.Marshal(instance + ":" + string(call.ID))
		call.Token, call.Version = connection.Token, caelisTaskBridgeVersion
		return forward(connection.Endpoint, call)
	})
}

func readTaskMCPConnection(path string) (taskMCPConnection, error) {
	var connection taskMCPConnection
	invalid := errors.New("Bot task MCP connection is unavailable")
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
	err = json.NewDecoder(io.LimitReader(file, 16<<10)).Decode(&connection)
	if err != nil || connection.Endpoint == "" || connection.Token == "" {
		return taskMCPConnection{}, invalid
	}
	return connection, nil
}
