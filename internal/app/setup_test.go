package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestFreshSetupDoesNotConnectAndDismissDoesNotChooseRuntime(t *testing.T) {
	root := t.TempDir()
	a, e := New(root, Host{})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if !a.NeedsSetup() || a.HasRuntimeChoice() {
		t.Fatal("fresh user bypassed choice")
	}
	if e = a.Backend.Connect(t.Context()); e == nil {
		t.Fatal("connected before selection")
	}
	if e = a.setup.Dismiss(); e != nil {
		t.Fatal(e)
	}
	if a.setup.Overview().Onboarding || a.HasRuntimeChoice() || !a.NeedsSetup() {
		t.Fatal("skip selected a runtime")
	}
	b, e := New(root, Host{})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if b.setup.Overview().Onboarding || b.HasRuntimeChoice() || !b.NeedsSetup() {
		t.Fatal("dismiss not retained")
	}
}
func TestRuntimeProfilesAndDraftsStaySeparate(t *testing.T) {
	root := t.TempDir()
	a, e := New(root, Host{})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ca := api.RuntimeSettings{Runtime: "caelis", CLIPath: "/custom/caelis", CaelisStore: "/custom/store"}
	if e = a.setup.saveProfile(ca); e != nil {
		t.Fatal(e)
	}
	got, e := a.setup.Profile("caelis")
	if e != nil || got != ca {
		t.Fatal(got, e)
	}
	got, e = a.setup.Profile("codex")
	if e != nil || got.Runtime != "codex" || got.CLIPath != "" || got.CaelisStore != "" {
		t.Fatal("profiles leaked", got, e)
	}
	if _, e = a.setup.Profile("../../outside"); e == nil {
		t.Fatal("invalid profile path accepted")
	}
	if e = os.WriteFile(filepath.Join(root, "draft.json"), []byte(`{"text":"Codex draft","referenceIds":[]}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"runtime":"caelis"}`), 0600); e != nil {
		t.Fatal(e)
	}
	b, e := New(root, Host{})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if b.ProviderDirectory() == root {
		t.Fatal("Caelis used Codex draft directory")
	}
	if v := b.Backend.Draft(); v.Text != "" {
		t.Fatal("Codex draft crossed provider", v)
	}
}
func TestRestartGuardPreservesUnknownWork(t *testing.T) {
	e := newTestEngine()
	a, _ := fixtureApp(t, e, Host{})
	defer a.Close()
	if err := a.Backend.PrepareRestart(func() error { return os.ErrPermission }); err == nil {
		t.Fatal("guard ignored")
	}
	a.Backend.CancelRestart()
	// Every subsequent submission is refused before resolving file bytes.
	if err := a.Backend.PrepareRestart(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Backend.Submit(t.Context(), api.Submission{ID: "no-dispatch"}); err == nil {
		t.Fatal("admission stayed open")
	}
}
