package app

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestThinNodeTaskTerminalHasNoLocalFallbackOrProductReplay(t *testing.T) {
	root := t.TempDir()
	if e := localstate.Write(filepath.Join(root, "product-connection.json"), productPairingDocument{Version: 1, Pairing: thinPairing()}); e != nil {
		t.Fatal(e)
	}
	a, e := New(root, Host{})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if got, e := a.WorkTerminal(t.Context(), "original-remote-task"); !errors.Is(e, api.ErrRemoteWorkTerminal) || got != (api.TerminalTarget{}) {
		t.Fatal("thin APP silently used local terminal", got, e)
	}
	if a.tasks != nil || a.personal != nil || a.started {
		t.Fatal("terminal observation initialized a local source")
	}
}

func TestRoamingTaskTerminalReportsMissingPinnedRemoteChannel(t *testing.T) {
	f := roamingControlFixture(t)
	if _, e := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-terminal-scope")); e != nil {
		t.Fatal(e)
	}
	if got, e := f.a.WorkTerminal(t.Context(), "original-remote-task"); !errors.Is(e, api.ErrRemoteWorkTerminal) || got != (api.TerminalTarget{}) {
		t.Fatal("roaming APP fell back to retired source terminal", got, e)
	}
	if len(f.client.commands) != 0 {
		t.Fatal("terminal observation issued a product command")
	}
}
