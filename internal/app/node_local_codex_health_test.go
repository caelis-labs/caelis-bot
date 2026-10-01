package app

import (
	"context"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
)

type localCodexHealthSetup struct{ api.SetupController }

func (*localCodexHealthSetup) Profile(id string) (api.RuntimeSettings, error) {
	return api.RuntimeSettings{Runtime: id}, nil
}
func (*localCodexHealthSetup) Inspect(context.Context, api.RuntimeSettings) (api.SetupState, error) {
	return api.SetupState{State: "ready", Installation: api.RuntimeStatus{Installed: true}}, nil
}

type localCodexHealthImpostor struct{ *testEngine }

func (*localCodexHealthImpostor) ProviderInfo() api.ProviderInfo {
	return api.ProviderInfo{ID: "codex"}
}
func (*localCodexHealthImpostor) OwnsLiveRuntime() bool {
	panic("generic capability is not native ownership")
}

func localCodexHealthApplication(t *testing.T, engine api.Engine, provider api.Engine) *Application {
	t.Helper()
	a := &Application{engine: engine, Backend: backend.NewService(provider, nil, nil, nil, nil)}
	a.Backend.ConfigureSetup(&localCodexHealthSetup{})
	var err error
	a.nodeRegistry, err = nodes.New("codex", engine.(api.WorkRuntime))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestLocalCodexHealthDoesNotInferOwnerFromReadySetupOrWorker(t *testing.T) {
	for _, tc := range []struct {
		name             string
		engine, provider api.Engine
	}{
		{name: "active disconnected Codex", engine: codex.NewSession(codex.SessionOptions{}), provider: codex.NewSession(codex.SessionOptions{})},
		{name: "inactive provider", engine: codex.NewSession(codex.SessionOptions{}), provider: newTestEngine()},
		{name: "non-native ownership claim", engine: &localCodexHealthImpostor{newTestEngine()}, provider: &localCodexHealthImpostor{newTestEngine()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := localCodexHealthApplication(t, tc.engine, tc.provider)
			health, err := nodeLocalHealth(t.Context(), a, api.NodeCodex)
			if err != nil || !health.AuthenticationKnown || !health.Authenticated || !health.HealthKnown || !health.Healthy || !health.WorkerEligible || health.SharedHost {
				t.Fatal("existing setup/Worker health semantics changed", health, err)
			}
			if health.ManagedOwner || health.Fenceable || health.BotEligible {
				t.Fatal("ready installation or Worker inferred managed Bot ownership", health)
			}
		})
	}
}
