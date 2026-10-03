// Package remotework owns target-side native workers. SSH is the authenticated
// boundary; requests reach only a per-application private Unix socket.
package remotework

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
)

const Version = 1

type Request struct {
	Version int                            `json:"version"`
	Runtime string                         `json:"runtime"`
	Action  string                         `json:"action"`
	ID      string                         `json:"id,omitempty"`
	Start   api.WorkStart                  `json:"start,omitempty"`
	Message api.TaskMessage                `json:"message,omitempty"`
	Model   api.WorkExecutionSettings      `json:"model,omitempty"`
	Team    api.RuntimeConfigurationChange `json:"team,omitempty"`
}
type Response struct {
	Version        int                        `json:"version"`
	Identity       string                     `json:"identity"`
	Available      []string                   `json:"available"`
	Setup          api.SetupState             `json:"setup"`
	Model          api.WorkExecutionSettings  `json:"model"`
	Models         []api.ModelOption          `json:"models"`
	RuntimeDefault *api.WorkExecutionSettings `json:"runtimeDefault"`
	Configuration  *api.RuntimeConfiguration  `json:"configuration,omitempty"`
	AdvancedIssue  string                     `json:"advancedIssue,omitempty"`
	States         []api.WorkState            `json:"states"`
	Task           api.Task                   `json:"task"`
	Terminal       api.TerminalTarget         `json:"terminal"`
	Mutation       api.RuntimeMutationResult  `json:"mutation"`
	Error          string                     `json:"error,omitempty"`
}

func Decode(data []byte, v any) error { return json.Unmarshal(data, v) }

func Socket(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(os.TempDir(), "caelis-owner-"+hex.EncodeToString(sum[:12]), "owner.sock")
}
