package backend

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

type modelSettingsEngine struct {
	executionFake
	calls            int
	catalogError     error
	changeError      error
	entered, release chan struct{}
}

func (e *modelSettingsEngine) Models(context.Context) ([]api.ModelOption, error) {
	return []api.ModelOption{{Model: "one", Efforts: []string{"low", "high"}, ServiceTiers: []api.ServiceTier{{ID: "priority"}}}, {Model: "two", Efforts: []string{"high"}, ServiceTiers: []api.ServiceTier{{ID: "priority"}}}}, e.catalogError
}
func (e *modelSettingsEngine) ChangeExecution(ctx context.Context, v api.ExecutionSettings, persist func() error) error {
	e.calls++
	if e.entered != nil {
		close(e.entered)
		<-e.release
		e.entered = nil
	}
	if err := e.executionFake.ChangeExecution(ctx, v, persist); err != nil {
		return err
	}
	return e.changeError
}

type workModelSettingsEngine struct {
	modelSettingsEngine
	work      api.WorkExecutionSettings
	workCalls int
}

func (e *workModelSettingsEngine) ChangeWorkExecution(_ context.Context, v api.WorkExecutionSettings, persist func() error) error {
	e.workCalls++
	if err := persist(); err != nil {
		return err
	}
	e.work = v
	return nil
}
func modelSettingsFixture(t *testing.T, e api.Engine) *Service {
	t.Helper()
	s := NewService(e, nil, nil, nil, nil)
	root := t.TempDir()
	s.ConfigureExecution(filepath.Join(root, "execution.json"), api.ExecutionSettings{Model: "one", Effort: "low", ServiceTier: "priority", ApprovalMode: "ask"})
	s.ConfigureWorkExecution(filepath.Join(root, "work.json"), api.WorkExecutionSettings{Model: "one", Effort: "high", ServiceTier: "priority"})
	return s
}
func TestModelSettingsAtomicCASPreservesPolicyAndExistingPersistence(t *testing.T) {
	e := &modelSettingsEngine{}
	s := modelSettingsFixture(t, e)
	state, _, err := s.ReadModelSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.Work != nil || state.ConversationDefault {
		t.Fatal("unsupported work/default advertised")
	}
	rev := productmanagement.ExecutionRevision(state)
	e.entered, e.release = make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.ApplyModelSettings(t.Context(), rev, "conversation", productmanagement.Selection{Model: "two", Effort: "high"})
	}()
	<-e.entered
	local := api.ExecutionSettings{Model: "one", Effort: "high", ServiceTier: "priority", ApprovalMode: "read-only"}
	localDone := make(chan error, 1)
	go func() { localDone <- s.SaveExecutionSettings(t.Context(), local) }()
	select {
	case <-localDone:
		t.Fatal("local save escaped settings ownership")
	default:
	}
	close(e.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-localDone; err != nil {
		t.Fatal(err)
	}
	got, err := LoadExecutionSettings(s.executionFile, api.ExecutionSettings{})
	if err != nil || got != local {
		t.Fatal(got, err)
	}
	if err := s.ApplyModelSettings(t.Context(), rev, "conversation", productmanagement.Selection{Model: "two", Effort: "high"}); !errors.Is(err, productmanagement.ErrExecutionConflict) {
		t.Fatal("stale CAS accepted", err)
	}
	if e.calls != 2 {
		t.Fatal("conflict dispatched", e.calls)
	}
	state, _, _ = s.ReadModelSettings(t.Context())
	if err := s.ApplyModelSettings(t.Context(), productmanagement.ExecutionRevision(state), "conversation", productmanagement.Selection{Model: "two", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ExecutionSettings()
	if got.Model != "two" || got.Effort != "high" || got.ServiceTier != "priority" || got.ApprovalMode != "read-only" {
		t.Fatal("model-only change altered policy", got)
	}
}
func TestModelSettingsCatalogFailuresRejectBeforeDispatchAndWorkReset(t *testing.T) {
	e := &workModelSettingsEngine{}
	s := modelSettingsFixture(t, e)
	state, _, _ := s.ReadModelSettings(t.Context())
	rev := productmanagement.ExecutionRevision(state)
	for _, v := range []productmanagement.Selection{{Model: "gone", Effort: "high"}, {Model: "two", Effort: "low"}, {Model: "two", Effort: ""}, {Effort: "high"}, {}} {
		if err := s.ApplyModelSettings(t.Context(), rev, "conversation", v); !errors.Is(err, productmanagement.ErrExecutionInvalid) {
			t.Fatal(v, err)
		}
	}
	if e.calls != 0 {
		t.Fatal("invalid selection dispatched")
	}
	e.catalogError = errors.New("private provider failure")
	if err := s.ApplyModelSettings(t.Context(), rev, "work", productmanagement.Selection{Model: "two", Effort: "high"}); !errors.Is(err, productmanagement.ErrExecutionUnavailable) || !errors.Is(err, e.catalogError) {
		t.Fatal("definite read failure was not retained/rejected", err)
	}
	e.catalogError = nil
	if err := s.ApplyModelSettings(t.Context(), rev, "work", productmanagement.Selection{}); err != nil {
		t.Fatal(err)
	}
	if e.work != (api.WorkExecutionSettings{}) || s.WorkExecutionSettings() != e.work {
		t.Fatal("inherit reset retained override", e.work)
	}
	got, _ := s.ExecutionSettings()
	if got != state.Conversation {
		t.Fatal("work reset changed conversation")
	}
}
func TestModelSettingsConcurrentRevisionHasOneWinner(t *testing.T) {
	e := &modelSettingsEngine{}
	s := modelSettingsFixture(t, e)
	state, _, _ := s.ReadModelSettings(t.Context())
	rev := productmanagement.ExecutionRevision(state)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			<-start
			errs <- s.ApplyModelSettings(t.Context(), rev, "conversation", productmanagement.Selection{Model: "two", Effort: "high"})
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	accepted, conflicted := 0, 0
	for err := range errs {
		if err == nil {
			accepted++
		} else if errors.Is(err, productmanagement.ErrExecutionConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflicted != 1 || e.calls != 1 {
		t.Fatal(accepted, conflicted, e.calls)
	}
}
