package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type workExecutionFake struct {
	snapshotEngine
	value  api.WorkExecutionSettings
	reject bool
}

func (e *workExecutionFake) ChangeWorkExecution(_ context.Context, v api.WorkExecutionSettings, persist func() error) error {
	if e.reject {
		return errors.New("unavailable work model")
	}
	if err := persist(); err != nil {
		return err
	}
	e.value = v
	return nil
}
func TestWorkExecutionPersistsIndependentlyAndOnlyAfterAcceptance(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "work-execution.json")
	v, err := LoadWorkExecutionSettings(path)
	if err != nil || v != (api.WorkExecutionSettings{}) {
		t.Fatal("upgrade should inherit Runtime", v, err)
	}
	e := &workExecutionFake{reject: true}
	s := NewService(e, nil, nil, nil, nil)
	bot := api.ExecutionSettings{Model: "bot-luna", Effort: "low", ApprovalMode: "auto"}
	s.ConfigureExecution(filepath.Join(root, "execution.json"), bot)
	s.ConfigureWorkExecution(path, v)
	manual := api.WorkExecutionSettings{Model: "work-sol", Effort: "high"}
	if err = s.SaveWorkExecutionSettings(t.Context(), manual); err == nil {
		t.Fatal("rejected work model saved")
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected write created file")
	}
	e.reject = false
	if err = s.SaveWorkExecutionSettings(t.Context(), manual); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWorkExecutionSettings(path)
	if err != nil || got != manual || s.WorkExecutionSettings() != manual || e.value != manual {
		t.Fatal("lost independent preference", got, err)
	}
	main, _ := s.ExecutionSettings()
	if main != bot {
		t.Fatal("work configuration changed Bot")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("settings not private")
	}
	s.workExecutionFile = path + "/cannot-write"
	if err = s.SaveWorkExecutionSettings(t.Context(), v); err == nil || e.value != manual || s.WorkExecutionSettings() != manual {
		t.Fatal("failed persistence applied setting")
	}
	s.workExecutionFile = path
	if err = s.SaveWorkExecutionSettings(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	if got, err = LoadWorkExecutionSettings(path); err != nil || got != v {
		t.Fatal("runtime selection did not clear override")
	}
	if err = os.WriteFile(path, []byte(`{"effort":"high"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadWorkExecutionSettings(path); err == nil {
		t.Fatal("invalid override silently inherited")
	}
}
