package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Real Host and native Worker; only the provider's decision is synthetic. Read
// the public state before delivering any review event to a fresh Bot projection.
func workerApprovalSnapshotAcceptance(t *testing.T, ctx context.Context, s *Session, host *client, model *acceptanceModel, mainModel, reviewerModel, effectDir string, review func(string, <-chan struct{})) {
	t.Helper()
	op := "native-worker-guardian-binding"
	var status wire.StatusSnapshot
	if err := host.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
		t.Fatal(err)
	}
	var catalog wire.AgentBindingStatus
	if err := host.json(ctx, "POST", "/agents/binding-status", struct{}{}, &catalog, "", ""); err != nil {
		t.Fatal(err)
	}
	var binding wire.AgentBinding
	for _, profile := range catalog.Targets {
		if value(profile.Id) == reviewerModel || value(profile.DisplayName) == "custom/reviewer-model" {
			binding = wire.AgentBinding{Handle: pointer("guardian"), ProfileId: profile.Id, Effort: profile.Effort.DefaultEffort}
		}
	}
	if binding.ProfileId == nil || value(binding.Effort) == "" {
		t.Fatalf("fixture Guardian profile missing: selector=%q targets=%+v", reviewerModel, catalog.Targets)
	}
	var result wire.CommandResult
	if err := host.json(ctx, "POST", "/agents/bind", wire.BindAgentBindingRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Binding: binding}, &result, op, string(status.Configuration.Revision)); err != nil || !succeeded(result.Outcome) {
		raw, _ := json.Marshal(result)
		t.Fatal("native Host Guardian binding", string(raw), err)
	}
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	review(`{"option_id":"allow_once"}`, gate)
	target := filepath.Join(effectDir, "native-snapshot-once.txt")
	const key = "CASE_NATIVE_SNAPSHOT"
	model.set(key, modelStep{Name: "RunCommand", Args: map[string]any{"command": "printf 'ONCE\\n' >> " + executionQuote(target), "sandbox_permissions": "require_escalated", "justification": "Write the synthetic snapshot acceptance fixture", "yield_time_ms": 1000}})
	s.mu.Lock()
	call := wire.SessionToolCall{SessionId: s.state.Session.SessionId, PrincipalId: s.state.PrincipalID, TurnId: "synthetic-parent", Source: wire.SessionSource{Kind: "user", OperationId: "native-snapshot-source"}}
	s.mu.Unlock()
	workerCtx := context.WithValue(ctx, invocationKey{}, call)
	task, err := s.StartWork(workerCtx, api.WorkStart{ID: "snapshot-worker", Workspace: t.TempDir(), ExecutionOverride: &api.WorkExecutionSettings{Model: mainModel}, TaskStart: api.TaskStart{RequestID: "native-snapshot-start", Title: "Native approval snapshot", Prompt: key}})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	w := clone(s.state.Workers[task.ID])
	s.mu.Unlock()
	if !w.Native || w.Binding.Profile.Permissions != nil || w.Binding.Profile.Reviewer != nil {
		t.Fatal("test replaced ordinary native Worker assembly")
	}
	waitAcceptance(t, ctx, func() bool {
		for _, r := range s.Snapshot().Reviews {
			if r.Owner == "task" && r.TaskTitle == task.Title && r.Status == "inProgress" {
				return true
			}
		}
		return false
	})
	for _, route := range []string{"state", "reconnect", "reconnect"} {
		var state wire.SessionState
		path := "/sessions/" + idPath(w.Binding.SessionId) + "/" + route
		if route == "state" {
			err = host.json(ctx, "GET", path, nil, &state, "", "")
		} else {
			err = host.stream(ctx, path, "", func(f frame) error {
				if f.event != "caelis.control.bootstrap" {
					return errors.New("missing first bootstrap frame")
				}
				if e := json.Unmarshal(f.data, &state); e != nil {
					return e
				}
				return io.EOF // Deliberately consume no following review event.
			})
			if errors.Is(err, io.EOF) {
				err = nil
			}
		}
		if err != nil || !value(state.Run.WaitingApproval) {
			t.Fatal("waiting native approval state", route, state.Run, err)
		}
		fresh := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("projection dispatched a request") })
		fresh.state.Workers[task.ID] = w
		fresh.state.Views[w.Binding.SessionId] = &view{State: state}
		if got := fresh.Snapshot().Approvals; len(got) != 0 {
			t.Fatalf("%s snapshot produced %d manual cards before review delivery (native active=%v)", route, len(got), state.Approval.Active != nil)
		}
		t.Logf("native %s: waiting=true, manual cards=0, review events delivered to fresh projection=0", route)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("native effect ran before Guardian's decision", err)
	}
	release()
	waitAcceptance(t, ctx, func() bool {
		for _, state := range s.WorkStates() {
			if state.Task.ID == task.ID {
				return state.Task.Status == "completed"
			}
		}
		return false
	})
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "ONCE\n" {
		t.Fatalf("native Guardian effect was not exactly once: %q %v", data, err)
	}
	t.Log("ordinary Native Worker: Guardian released the original request, effect=ONCE")
}
