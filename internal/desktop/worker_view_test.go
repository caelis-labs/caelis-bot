package desktop

import (
	"context"
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

type assemblyWorkerTasks []api.Task

func (f assemblyWorkerTasks) ListTasks() []api.Task { return f }
func (assemblyWorkerTasks) ReadTaskConversation(context.Context, string) (api.TaskConversation, error) {
	return api.TaskConversation{}, errors.New("routing must not read history")
}
func (assemblyWorkerTasks) ObserveTask(context.Context, string) (api.WorkObservation, error) {
	return api.WorkObservation{}, errors.New("routing must not observe a worker")
}

// Keep the production assembly callback: a stub workerTaskView hid the local
// terminal regression. Only the task catalog and native effects are controlled.
func workerRoutingAssembly(t *testing.T) (*productAssembly, *int) {
	t.Helper()
	a, err := newProductAssembly(t.TempDir(), []string{"en"}, func(*Service, *diagnosticlog.Logger) app.Host { return app.Host{} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Core.Close() })
	s := a.Service
	s.needsIntroduction = func() bool { return false }
	s.start(&fakeDriver{displays: []Rect{{0, 40, 1440, 860}}})
	t.Cleanup(s.shutdown)
	a.Core.Backend.ConfigureWorkerTaskView(assemblyWorkerTasks{
		{ID: "local-codex", MachineName: "My Mac", Status: "working"},
		{ID: "local-caelis", Status: "completed"},
		// A display name is never the environment authority.
		{ID: "peer", Machine: "machine-original-peer", MachineName: "本机", Status: "working"},
	})
	s.observeTasks([]api.TaskPreview{
		{ID: "local-codex", Provider: "codex"},
		{ID: "local-caelis", Provider: "caelis"},
		{ID: "peer"}, {ID: "stale"},
	})
	opened := 0
	s.openHistory = func() { opened++ }
	return a, &opened
}

func TestAssemblyWorkerCardRoutesLocalTerminalAndRemoteViewer(t *testing.T) {
	a, viewed := workerRoutingAssembly(t)
	s := a.Service
	var terminal []string
	s.taskWindows = &taskControllerStub{click: func(_ context.Context, id string) error {
		terminal = append(terminal, id)
		return nil
	}}
	for _, id := range []string{"local-codex", "local-caelis", "local-codex"} {
		if err := s.OpenTaskTerminal(id); err != nil {
			t.Fatal(err)
		}
	}
	if len(terminal) != 3 || terminal[0] != "local-codex" || terminal[1] != "local-caelis" || terminal[2] != "local-codex" || *viewed != 0 || s.WorkerTaskSelection() != "" {
		t.Fatalf("local task did not reach its original terminal: %v, viewed=%d", terminal, *viewed)
	}
	s.taskWindows = nil // Remote viewing needs no local terminal capability.
	if err := s.OpenTaskTerminal("peer"); err != nil || *viewed != 1 || s.WorkerTaskSelection() != "peer" {
		t.Fatalf("remote task did not open its viewer: %v, viewed=%d", err, *viewed)
	}
	s.CloseWorkerTask()
	if s.WorkerTaskSelection() != "" {
		t.Fatal("view selection survived close")
	}
	for _, id := range []string{"foreign", "stale", "local-codex"} {
		if err := s.OpenTaskTerminal(id); err == nil {
			t.Fatalf("%s opened without an owned task or native terminal", id)
		}
	}
	if *viewed != 1 || len(terminal) != 3 {
		t.Fatal("unavailable terminal silently fell back to history")
	}
}

func TestAssemblyLocalTerminalFailureDoesNotOpenHistory(t *testing.T) {
	a, viewed := workerRoutingAssembly(t)
	cause := errors.New("native terminal capability unavailable")
	a.Service.taskWindows = &taskControllerStub{click: func(context.Context, string) error { return cause }}
	if err := a.Service.OpenTaskTerminal("local-codex"); !errors.Is(err, cause) || *viewed != 0 {
		t.Fatalf("native failure was replaced with a viewer: %v, viewed=%d", err, *viewed)
	}
	a.Service.shutdown()
	if err := a.Service.OpenTaskTerminal("peer"); err == nil || *viewed != 0 {
		t.Fatal("stopped surface opened a remote window")
	}
}
