package app

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

// Provider construction is separate from both native surfaces and protocol
// adapters. Only complete, registered adapters may become a product connection.
type providerConfig struct {
	Settings                                  api.RuntimeSettings
	Execution                                 api.ExecutionSettings
	WorkDirectory, ConversationFile, WorkRoot string
}
type providerFactory struct {
	ID       string
	Defaults api.ExecutionSettings
	Open     func(providerConfig) (api.Engine, error)
}
type factoryResolver func(string) (providerFactory, error)

func resolveProvider(id string) (providerFactory, error) {
	if id == "caelis" {
		return providerFactory{ID: id, Defaults: api.ExecutionSettings{ApprovalMode: "workspace-write"}, Open: func(c providerConfig) (api.Engine, error) {
			return caelis.New(caelis.Options{Directory: filepath.Dir(c.ConversationFile), Settings: c.Settings, ApplicationOwned: true}), nil
		}}, nil
	}
	if id != "codex" {
		return providerFactory{}, errors.New("该后端尚未接入，原有连接记录保持不变")
	}
	return providerFactory{ID: id, Defaults: api.ExecutionSettings{ApprovalMode: "auto"}, Open: func(c providerConfig) (api.Engine, error) {
		if err := codex.ValidateSettings(c.Settings, c.Execution); err != nil {
			return nil, err
		}
		binary := c.Settings.CLIPath
		if override := os.Getenv("CODEX_BIN"); override != "" {
			binary = override
		}
		return codex.NewSession(codex.SessionOptions{Binary: binary, Socket: os.Getenv("CAELIS_CODEX_SOCKET"),
			Execution: c.Execution, Directory: c.WorkDirectory, WorkRoot: c.WorkRoot, StateFile: c.ConversationFile}), nil
	}}, nil
}

var providerName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Codex keeps its existing on-disk layout. That legacy namespace belongs only
// to Codex; new providers can never inherit its native IDs, receipts or files.
// No copying/moving live task bindings is needed for this boundary refactor.
func providerDirectory(root, id string) (string, error) {
	if !providerName.MatchString(id) {
		return "", errors.New("无效的后端标识")
	}
	if id == "codex" {
		return root, nil
	}
	return filepath.Join(root, "providers", id), nil
}
