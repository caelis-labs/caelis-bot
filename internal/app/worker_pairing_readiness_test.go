package app

import (
	"context"
	"encoding/json"
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
	for range 2 {
		if err = s.PairingReadiness(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadFile(pool.path)
	if calls != 0 || string(after) != string(original) {
		t.Fatal("readiness created owner or changed config")
	}
}

type catalogReadFailure struct{ *workerFixture }

func (f *catalogReadFailure) Models(context.Context) ([]api.ModelOption, error) {
	var v any
	err := json.Unmarshal([]byte("native invalid json"), &v)
	return nil, err
}
func TestNativeCatalogJSONFailureIsNotMissingLocalConfiguration(t *testing.T) {
	root := t.TempDir()
	base := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": base})
	pool.resident = &catalogReadFailure{base}
	if code := api.WorkerReadinessCode(pool.workerReadiness(t.Context())); code != "worker_model_catalog_unavailable" {
		t.Fatal("native protocol failure was blamed on local settings", code)
	}
	pool.resident = base
	path := filepath.Join(root, "work-execution.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := api.WorkerReadinessCode(pool.workerReadiness(t.Context())); code != "worker_configuration_invalid" {
		t.Fatal("local malformed settings not classified at their source", code)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != "{" {
		t.Fatal("readiness repaired user settings", err)
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
