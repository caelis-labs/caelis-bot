package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
)

func notebookSettingsApp(t *testing.T) *Application {
	t.Helper()
	return &Application{root: t.TempDir(), Backend: backend.NewService(nil, nil, nil, nil, nil)}
}
func TestDefaultNotebookAssemblyIsDormant(t *testing.T) {
	a := notebookSettingsApp(t)
	if err := attachDefaultNotebookSync(a); err != nil {
		t.Fatal(err)
	}
	settings, err := a.Backend.NotebookSyncSettings()
	if err != nil || settings.Enabled || settings.IntervalMinutes != 5 || a.notebookSync != nil {
		t.Fatal(settings, err)
	}
	if err = a.notebookSyncStartupGuard(); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Backend.SyncNotebook(context.Background(), "unselected"); err == nil {
		t.Fatal("unselected backup dispatched")
	}
}
func TestSavedNotebookIntentBlocksOldSourceEvenWithDisabledSettings(t *testing.T) {
	for _, phase := range []string{"stopping", "stopped", "starting", "switched"} {
		t.Run(phase, func(t *testing.T) {
			a := notebookSettingsApp(t)
			intent := notebooksync.State{SourceNodeID: "local", Targets: []notebooksync.Status{{NodeID: "standby", Phase: phase}}}
			if err := localstate.Write(filepath.Join(a.root, "nodeplane", "notebook-sync.json"), intent); err != nil {
				t.Fatal(err)
			}
			if err := attachDefaultNotebookSync(a); err != nil {
				t.Fatal(err)
			}
			if err := a.notebookSyncStartupGuard(); err == nil {
				t.Fatal("old source restarted through disabled sync settings")
			}
			if _, err := a.Backend.SaveNotebookSyncSettings(context.Background(), backend.NotebookSyncSettings{IntervalMinutes: 5}); err == nil {
				t.Fatal("original switch fence removed")
			}
		})
	}
}
