package app

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
)

// NewOwnedResident assembles the ordinary complete Bot profile. A designated
// Caelis store uses the existing owned Host boundary; no snapshot or lease is
// required. Codex keeps its standard target-local App Server and authentication.
func NewOwnedResident(ctx context.Context, root string, host Host, nodeID, helper string) (*Application, error) {
	remote, err := RemoteProductSelected(root)
	if err != nil || remote || nodeID == "" || !filepath.IsAbs(helper) {
		return nil, errors.New("target-local resident owner required")
	}
	resolve := func(id string) (providerFactory, error) {
		f, err := resolveProvider(id)
		if err != nil || id != "caelis" {
			return f, err
		}
		f.Open = func(c providerConfig) (api.Engine, error) {
			return caelis.NewOwned(ctx, caelis.Options{Diagnostics: c.Diagnostics, Directory: filepath.Dir(c.ConversationFile), Settings: c.Settings, Execution: c.Execution, WorkExecution: c.WorkExecution}, caelis.OwnedHostOptions{NodeID: nodeID, Binary: c.Settings.CLIPath, Store: c.Settings.CaelisStore, WatchdogHelper: helper})
		}
		return f, nil
	}
	a, err := newApplication(root, host, resolve)
	if err != nil {
		return nil, err
	}
	if err = AttachNodeManagement(a); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}

// StopNotebookOwner freezes new work and requires the concrete owned Runtime
// to finish stopping. Ordinary shared Hosts cannot provide this proof.
func (a *Application) StopNotebookOwner(ctx context.Context) error {
	hooks, completed := a.NotebookLocalSourceHooks()
	if err := hooks.StopSource(ctx); err != nil {
		return err
	}
	body, err := completed(ctx)
	if err != nil {
		return err
	}
	return localstate.Write(filepath.Join(a.root, "Product", "completed-handoff.json"), body)
}
