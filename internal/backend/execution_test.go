package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type executionFake struct {
	snapshotEngine
	reject bool
	value  api.ExecutionSettings
}

func (e *executionFake) Models(context.Context) ([]api.ModelOption, error) {
	return []api.ModelOption{}, nil
}
func (e *executionFake) ChangeExecution(_ context.Context, v api.ExecutionSettings, persist func() error) error {
	if e.reject {
		return errors.New("unsupported selection")
	}
	if err := persist(); err != nil {
		return err
	}
	e.value = v
	return nil
}
func TestExecutionPreferencesOnlyPersistAcceptedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.json")
	initial, err := LoadExecutionSettings(path)
	if err != nil || initial.ApprovalMode != "auto" || initial.Model != "" {
		t.Fatal(initial, err)
	}
	engine := &executionFake{reject: true}
	service := NewService(engine, nil, nil, nil, nil)
	service.ConfigureExecution(path, initial)
	value := api.ExecutionSettings{Model: "catalog-model", Effort: "high", ServiceTier: "fast", ApprovalMode: "ask"}
	if err = service.SaveExecutionSettings(context.Background(), value); err == nil {
		t.Fatal("unsupported model persisted")
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) || service.ExecutionSettings() != initial {
		t.Fatal("rejection changed settings")
	}
	engine.reject = false
	if err = service.SaveExecutionSettings(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadExecutionSettings(path)
	if err != nil || loaded != value || engine.value != value {
		t.Fatal("restart lost selection", loaded, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe settings file", err)
	}
	service.executionFile = path + "/impossible"
	change := value
	change.ApprovalMode = "full-access"
	if err = service.SaveExecutionSettings(context.Background(), change); err == nil || engine.value != value || service.ExecutionSettings() != value {
		t.Fatal("disk failure mutated execution")
	}
	if err = os.WriteFile(path, []byte(`{"approvalMode":"not-a-policy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadExecutionSettings(path); err == nil {
		t.Fatal("invalid stored policy silently accepted")
	}
}
