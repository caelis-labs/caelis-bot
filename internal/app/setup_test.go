package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

func TestCancelPreparationDoesNotWaitForInstallerLock(t *testing.T) {
	s := &runtimeSetup{}
	ctx,cancel := context.WithCancel(context.Background())
	s.operationCancel=cancel
	s.mu.Lock()
	done := make(chan bool,1)
	go func(){ done<-s.CancelOperation() }()
	select {
	case cancelled := <-done:
		if !cancelled || ctx.Err()!=context.Canceled { t.Fatal("setup operation was not cancelled") }
	case <-time.After(time.Second): t.Fatal("cancel waited for installer operation to finish")
	}
	s.mu.Unlock()
}

func TestInstallProgressCanBeReadDuringSetupOperation(t *testing.T) {
	s := &runtimeSetup{}
	s.mu.Lock() // Apply holds this while the installer runs.
	s.setInstallProgress(caelisruntime.InstallProgress{Phase: "downloading", Downloaded: 1024, Total: 2048, BytesPerSecond: 512})
	result := make(chan api.SetupInstallProgress, 1)
	go func() { result <- s.InstallProgress() }()
	select {
	case p := <-result:
		if p.Phase != "downloading" || p.Downloaded != 1024 || p.Total != 2048 || p.BytesPerSecond != 512 {
			t.Fatalf("lost installer measurement: %+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("progress read blocked behind the installer lock")
	}
	s.mu.Unlock()
}

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
	if overview := a.setup.Overview(); overview.Active != "codex" || overview.HasRuntimeChoice {
		t.Fatalf("default provider must not imply a selection: %+v", overview)
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
	if overview := b.setup.Overview(); overview.Active != "codex" || overview.HasRuntimeChoice {
		t.Fatalf("dismissed setup selected default provider: %+v", overview)
	}
}
func TestFirstRunStagesProviderBeforeConnectionWithoutChoosingRuntime(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	if restart, err := a.setup.StageRuntime("caelis"); err != nil || restart {
		t.Fatalf("first choice must stay in this process: %v %v", restart, err)
	}
	if a.HasRuntimeChoice() {
		t.Fatal("staging was mistaken for an activated Runtime")
	}
	if _, err = os.Stat(filepath.Join(root, "runtime.json")); !os.IsNotExist(err) {
		t.Fatal("staging wrote the active connection", err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := New(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := b.setup.Overview(); got.Active != "caelis" || got.Pending != "caelis" || got.HasRuntimeChoice || !got.Onboarding {
		t.Fatalf("staged provider did not resume first-run setup: %+v", got)
	}
	if restart, err := b.setup.StageRuntime("caelis"); err != nil || restart {
		t.Fatalf("same provider must stay in process: %v %v", restart, err)
	}
	if restart, err := b.setup.StageRuntime("codex"); err != nil || restart {
		t.Fatalf("changing choice must stay in this process: %v %v", restart, err)
	}
	if _, err := b.setup.StageRuntime("../bad"); err == nil {
		t.Fatal("invalid staged provider accepted")
	}
}
func TestFreshConversationJournalDoesNotChooseRuntimeAfterRestart(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bot.json"), []byte(`{"version":1,"personalVersion":1,"id":"fresh"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "conversation.json"), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.HasRuntimeChoice() || !a.NeedsSetup() || !a.setup.Overview().Onboarding {
		t.Fatal("conversation journal skipped runtime selection")
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
	if overview := b.setup.Overview(); overview.Active != "caelis" || !overview.HasRuntimeChoice {
		t.Fatalf("persisted selection missing from overview: %+v", overview)
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
