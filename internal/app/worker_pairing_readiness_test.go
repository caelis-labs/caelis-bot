package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func TestPairingInspectsExistingWorkerWithoutForeignReadinessOwner(t *testing.T) {
	root := t.TempDir()
	resident := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": resident})
	pool.defaultRuntime = "conversation"
	if err := localstate.Write(pool.path, map[string]any{"version": 1, "runtime": "conversation"}); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(pool.path)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s, err := workerpeer.Open(filepath.Join(root, "peers"), func(context.Context, string, string, string) (workerpeer.Owner, string, *api.WorkExecutionSettings, error) {
		calls++
		return nil, "", nil, errors.New("foreign owner must not be opened by pairing readiness")
	})
	if err != nil {
		t.Fatal(err)
	}
	s.ConfigureReadiness(pool.workerReadiness)
	if err = s.Enable(true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		v, err := s.Pairing(t.Context())
		if err != nil || v.Code == "" {
			t.Fatal("existing local configuration not ready", err)
		}
	}
	after, err := os.ReadFile(pool.path)
	if err != nil || string(original) != string(after) || calls != 0 || len(pool.owners) != 0 || resident.starts != 0 || resident.connections != 0 || pool.defaultRuntime != "conversation" {
		t.Fatal("pairing mutated settings/allocated execution", calls, err)
	}
	if _, err = os.Stat(filepath.Join(root, "peers", "readiness")); !os.IsNotExist(err) {
		t.Fatal("long-lived readiness owner path created", err)
	}
}
func TestPairingReadinessHonorsBoundedWaitAndDoesNotRepairInvalidConfiguration(t *testing.T) {
	root := t.TempDir()
	resident := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": resident})
	pool.settingsMu.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := pool.workerReadiness(ctx)
	pool.settingsMu.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("ready wait ignored caller deadline", err)
	}
	dir, err := providerDirectory(root, "codex")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "work-execution.json")
	if err = localstate.Write(path, api.WorkExecutionSettings{Model: "missing-configured-model"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.workerReadiness(t.Context()); err == nil {
		t.Fatal("invalid configured model advertised ready")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) || pool.defaultRuntime != "codex" {
		t.Fatal("readiness silently repaired/selected configuration", err)
	}
}
