package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// NativeOwnedCaelisSetupSettings is a target-native constructor port, never a
// renderer method. It borrows only this managed node's existing owned Host;
// reading settings neither starts a Host nor transfers its lifetime to a wizard.
func NativeOwnedCaelisSetupSettings(ctx context.Context, a *Application, nodeID string) (api.RuntimeSettings, error) {
	if a == nil {
		return api.RuntimeSettings{}, errors.New("managed Caelis setup owner unavailable")
	}
	a.mu.Lock()
	managed, engine := a.managed, a.engine
	a.mu.Unlock()
	if managed == nil || managed.nodeID != nodeID {
		return api.RuntimeSettings{}, errors.New("exact managed Caelis node required")
	}
	provider, ok := engine.(api.Provider)
	if !ok || provider.ProviderInfo().ID != "caelis" {
		return api.RuntimeSettings{}, errors.New("managed node has no owned Caelis Host")
	}
	owner, ok := engine.(interface {
		OwnedSetupSettings(context.Context) (api.RuntimeSettings, error)
	})
	if !ok {
		return api.RuntimeSettings{}, errors.New("owned Caelis setup authority unavailable")
	}
	settings, e := owner.OwnedSetupSettings(ctx)
	if e != nil {
		return api.RuntimeSettings{}, e
	}
	if settings.Runtime != "caelis" || !filepath.IsAbs(settings.CLIPath) || !filepath.IsAbs(settings.CaelisStore) {
		return api.RuntimeSettings{}, errors.New("owned Caelis setup designation changed")
	}
	return settings, nil
}
