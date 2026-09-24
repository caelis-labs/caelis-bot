package app

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestConnectionProfileBeforeActivation(t *testing.T) {
	// Preparing another Runtime must not depend on the active Bot backend.
	setup := &runtimeSetup{}
	want := api.RuntimeSettings{Runtime: "caelis", CLIPath: "/fixture/caelis", CaelisStore: "/fixture/store"}
	got, err := setup.connectionProfile(&want)
	if err != nil || got != want {
		t.Fatalf("connection profile = %+v, %v; want %+v", got, err, want)
	}
	for _, invalid := range []api.RuntimeSettings{
		{Runtime: "codex"},
		{Runtime: "caelis", CLIPath: "relative/caelis"},
		{Runtime: "caelis", CaelisStore: "relative/store"},
	} {
		if _, err := setup.connectionProfile(&invalid); err == nil {
			t.Errorf("accepted invalid setup profile %+v", invalid)
		}
	}
}
