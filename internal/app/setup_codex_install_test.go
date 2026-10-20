package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

func TestOtherInstallerProgressDoesNotEraseCodexRecovery(t *testing.T) {
	c := newCodexInstallation(t.TempDir())
	c.progress = api.SetupInstallProgress{Runtime: "codex", ID: "original", State: "unknown"}
	s := &runtimeSetup{codexInstallation: c, progress: api.SetupInstallProgress{Runtime: "caelis", State: "running"}}
	s.setInstallProgress(caelisruntime.InstallProgress{Phase: "downloading", Downloaded: 10, Total: 20})
	if p := s.InstallProgress(); p.Runtime != "caelis" || p.Downloaded != 10 || p.Total != 20 {
		t.Fatal("active installer lost measured progress", p)
	}
	s.progress.State = "complete"
	if p := s.InstallProgress(); p.ID != "original" || p.State != "unknown" {
		t.Fatal("another runtime erased the unconfirmed Codex operation", p)
	}
}

func TestCodexInstallationSharedAcrossRequestsAndDisconnect(t *testing.T) {
	c := newCodexInstallation(t.TempDir())
	defer c.close()
	request := api.SetupRequest{Settings: api.RuntimeSettings{Runtime: "codex"}, Action: "install"}
	ctx, disconnect := context.WithCancel(t.Context())
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	run := func(ctx context.Context, _ api.SetupRequest) (api.SetupState, error) {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return api.SetupState{}, ctx.Err()
		case <-finish:
			return api.SetupState{}, nil
		}
	}
	first, err := c.start(ctx, request, run)
	if err != nil || first.State != "installing" {
		t.Fatal(first, err)
	}
	<-started
	id := c.snapshot().ID
	disconnect()
	var tabs sync.WaitGroup
	for range 8 {
		tabs.Go(func() {
			if _, err := c.start(t.Context(), request, run); err != nil {
				t.Error(err)
			}
		})
	}
	tabs.Wait()
	if calls.Load() != 1 || c.snapshot().ID != id || c.snapshot().State != "running" {
		t.Fatal("another client restarted or cancelled installation")
	}
	if state, active := c.runningState(request.Settings); !active || state.State != "installing" {
		t.Fatal("inspection did not expose current operation")
	}
	close(finish)
	<-c.done
	if c.snapshot().State != "complete" {
		t.Fatal(c.snapshot())
	}
}

func TestCodexInstallationFailureRequiresExplicitRetryAndPersists(t *testing.T) {
	root := t.TempDir()
	c := newCodexInstallation(root)
	defer c.close()
	r := api.SetupRequest{Settings: api.RuntimeSettings{Runtime: "codex"}, Action: "install"}
	_, err := c.start(t.Context(), r, func(context.Context, api.SetupRequest) (api.SetupState, error) {
		return api.SetupState{}, errors.New("SECRET_URL_AND_TOKEN")
	})
	if err != nil {
		t.Fatal(err)
	}
	<-c.done
	failed := c.snapshot()
	if failed.State != "failed" || failed.Error == "" || failed.Error == "SECRET_URL_AND_TOKEN" {
		t.Fatal(failed)
	}
	reloaded := newCodexInstallation(root)
	if reloaded.snapshot() != failed {
		t.Fatal("lost terminal status")
	}
	_, err = c.start(t.Context(), r, func(context.Context, api.SetupRequest) (api.SetupState, error) {
		return api.SetupState{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-c.done
	if c.snapshot().ID == failed.ID || c.snapshot().State != "complete" {
		t.Fatal(c.snapshot())
	}
}

func TestCodexInstallationCancellationAndRestartNeverReplay(t *testing.T) {
	root := t.TempDir()
	c := newCodexInstallation(root)
	r := api.SetupRequest{Settings: api.RuntimeSettings{Runtime: "codex"}, Action: "install"}
	_, err := c.start(t.Context(), r, func(ctx context.Context, _ api.SetupRequest) (api.SetupState, error) {
		<-ctx.Done()
		return api.SetupState{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	reloaded := newCodexInstallation(root)
	if reloaded.snapshot().State != "unknown" || reloaded.cancel != nil || reloaded.snapshot().ID != c.snapshot().ID {
		t.Fatal("restart hid uncertainty or replayed installation")
	}
	if _, err := reloaded.start(t.Context(), r, nil); err == nil {
		t.Fatal("unknown original installation authorized another write")
	}
	c.close()
	if c.snapshot().State != "cancelled" {
		t.Fatal(c.snapshot())
	}
	if _, err := c.start(t.Context(), r, nil); err == nil {
		t.Fatal("closed owner accepted work")
	}
}

func TestCodexInstallationPreservesKnownPreconditionFailure(t *testing.T) {
	root := t.TempDir()
	c := newCodexInstallation(root)
	defer c.close()
	_, err := c.start(t.Context(), api.SetupRequest{Settings: api.RuntimeSettings{Runtime: "codex"}, Action: "install"}, func(context.Context, api.SetupRequest) (api.SetupState, error) {
		return api.SetupState{}, &api.SetupBlockedError{Code: "runtime_recovery_required", Message: "原工作需要核对"}
	})
	if err != nil {
		t.Fatal(err)
	}
	<-c.done
	v := newCodexInstallation(root).snapshot()
	if v.State != "failed" || v.Code != "runtime_recovery_required" || v.Error != "原工作需要核对" || v.ExitCode != 0 {
		t.Fatal(v)
	}
}
