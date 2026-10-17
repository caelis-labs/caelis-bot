package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/windowsupdate"
)

func TestWindowsUpdateRetryRequiresTerminalFailureReceipt(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Updates", "original")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(root, "windows-update-pending.json")
	if err := localstate.Write(pending, map[string]string{"directory": dir}); err != nil {
		t.Fatal(err)
	}
	if retireFailedWindowsUpdate(root) == nil {
		t.Fatal("missing outcome was replayable")
	}
	for _, state := range []string{"waiting", "installing", "installed", "failed"} {
		receipt := filepath.Join(dir, "result.json")
		if err := localstate.Write(receipt, windowsupdate.ApplyResult{State: state, Version: "0.14.0-windows.preview.2"}); err != nil {
			t.Fatal(err)
		}
		err := retireFailedWindowsUpdate(root)
		if (err == nil) != (state == "failed") {
			t.Fatal(state, err)
		}
		if _, err := os.Stat(receipt); err != nil {
			t.Fatal("original receipt was removed", err)
		}
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatal("terminal failure did not release pending pointer", err)
	}
}

type updatePreflightFixture struct {
	entered, release chan struct{}
	cancels          atomic.Int32
}

func (f *updatePreflightFixture) PrepareUpdate() error { close(f.entered); <-f.release; return nil }
func (f *updatePreflightFixture) CancelUpdate()        { f.cancels.Add(1) }
func (f *updatePreflightFixture) Close() error {
	panic("failed terminal closure must not stop the application")
}

func TestWindowsUpdateClaimsOnceAndResumesAdmissionAfterTerminalFailure(t *testing.T) {
	core := &updatePreflightFixture{entered: make(chan struct{}), release: make(chan struct{})}
	resumed := make(chan struct{})
	var pauses atomic.Int32
	u := &windowsUpdater{service: &Service{}, core: core, context: context.Background(), state: UpdatePreferences{State: "ready"}, offer: &windowsupdate.Offer{}, pauseTerminals: func() func() { pauses.Add(1); return func() { close(resumed) } }, closeTerminals: func(context.Context) error { return errors.New("original observer not confirmed") }}
	if err := u.install(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-core.entered:
	case <-time.After(time.Second):
		t.Fatal("preflight did not start")
	}
	if u.install() == nil {
		t.Fatal("duplicate update admitted")
	}
	close(core.release)
	select {
	case <-resumed:
	case <-time.After(time.Second):
		t.Fatal("terminal admission stayed paused")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.state.Waiting || u.state.State != "ready" || core.cancels.Load() != 1 || pauses.Load() != 1 {
		t.Fatal("failed handoff did not restore product admission", u.state)
	}
}
