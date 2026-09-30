package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
)

type sourceFixture struct {
	source api.WorkDispatchSource
	err    error
	calls  int
}

func (s *sourceFixture) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	s.calls++
	return s.source, s.err
}

type remoteFixture struct {
	*fixtureRuntime
	ledger                                  string
	resolves, prepares, reads, sends, stops int
	prepareErr                              error
	lastMessage                             api.TaskMessage
	requests                                map[string]api.TaskMessage
}

func (f *remoteFixture) ResolveWorkWorkspace(_ context.Context, id, requested string) (string, error) {
	f.resolves++
	if requested != "" {
		return requested, nil
	}
	return "/target-private/Tasks/" + id, nil
}
func (f *remoteFixture) PrepareWorkWorkspace(_ context.Context, id, workspace string, selected bool) error {
	f.prepares++
	b, err := os.ReadFile(f.ledger)
	if err != nil {
		return errors.New("workspace mutation preceded durable intent")
	}
	var state state
	if json.Unmarshal(b, &state) != nil || state.Records[id] == nil || state.Records[id].View.Workspace != workspace || state.Records[id].RequestDigest == "" {
		return errors.New("wrong durable workspace binding")
	}
	return f.prepareErr
}
func (f *remoteFixture) ReadWork(ctx context.Context, id string) (api.Task, error) {
	f.reads++
	return f.fixtureRuntime.ReadWork(ctx, id)
}
func (f *remoteFixture) StopWork(ctx context.Context, id string) (api.Task, error) {
	f.stops++
	return f.fixtureRuntime.StopWork(ctx, id)
}
func (f *remoteFixture) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	f.lastMessage = in
	if previous, ok := f.requests[in.RequestID]; ok {
		if previous.Source != in.Source || previous.RequestDigest != in.RequestDigest {
			return api.Task{}, errors.New("changed original receipt binding")
		}
		return f.states[in.ID].Task, nil
	}
	f.sends++
	f.requests[in.RequestID] = in
	return f.fixtureRuntime.SendWork(ctx, in)
}

func routedFixture(t *testing.T) (*Manager, *fixtureRuntime, *remoteFixture, *sourceFixture, *nodes.Registry, api.WorkTarget) {
	t.Helper()
	root := t.TempDir()
	local := newRuntime()
	remote := &remoteFixture{fixtureRuntime: newRuntime(), ledger: filepath.Join(root, "tasks.json"), requests: map[string]api.TaskMessage{}}
	source := &sourceFixture{source: api.WorkDispatchSource{NodeID: api.LocalNodeID, Backend: "codex", Kind: "native_activation", BindingID: "resident-thread", OperationID: "resident-turn"}}
	router, err := nodes.New("codex", local)
	if err != nil {
		t.Fatal(err)
	}
	target := api.WorkTarget{NodeID: "linux-a", Backend: "caelis", Role: api.RoleWorker}
	if err = router.Set(nodes.Node{ID: "linux-a", Label: "Linux A", OS: "linux"}, nodes.Capability{Target: target, State: nodes.Ready}, remote); err != nil {
		t.Fatal(err)
	}
	m, err := OpenRouted(remote.ledger, filepath.Join(root, "Tasks"), "codex", local, local, local.Snapshot, router, source)
	if err != nil {
		t.Fatal(err)
	}
	return m, local, remote, source, router, target
}

func TestCrossBackendWorkerUsesTargetWorkspaceAndExactOwnedRoute(t *testing.T) {
	m, local, remote, source, _, target := routedFixture(t)
	in := input("cross-backend-request")
	in.Target = &target
	in.Workspace = "/target-only/project"
	v, err := m.StartTask(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if local.starts != 0 || remote.starts != 1 || remote.prepares != 1 || v.Workspace != in.Workspace || v.Target == nil || *v.Target != target || remote.lastStart.Source != source.source || len(remote.lastStart.RequestDigest) != 64 {
		t.Fatal(v, remote.lastStart)
	}
	if _, err = os.Stat(m.root); !os.IsNotExist(err) {
		t.Fatal("client allocated remote workspace", err)
	}
	if !m.OwnsWorkTarget(v.ID, target) || m.OwnsWorkTarget(v.ID, localTarget("codex")) {
		t.Fatal("wrong ledger target ownership")
	}
	v.Target.NodeID = "tampered-return-value"
	if !m.OwnsWorkTarget(v.ID, target) {
		t.Fatal("returned DTO changed authority")
	}
	if _, err = m.ReadTask(t.Context(), v.ID); err != nil || remote.reads != 1 {
		t.Fatal("read did not use original route", err)
	}
	msg := api.TaskMessage{ID: v.ID, RequestID: "continue-cross-backend", Prompt: "Continue", Source: api.WorkDispatchSource{Kind: "fabricated"}}
	if _, err = m.SendTask(t.Context(), msg); err != nil || remote.lastMessage.Source != source.source {
		t.Fatal("message bypassed authorizer", err)
	}
	source.source.OperationID = "later-turn"
	if _, err = m.SendTask(t.Context(), msg); err != nil || remote.sends != 1 || remote.lastMessage.Source.OperationID != "resident-turn" {
		t.Fatal("receipt source changed on retry", err)
	}
	msg.Prompt = "Changed continuation"
	if _, err = m.SendTask(t.Context(), msg); err == nil || remote.sends != 1 {
		t.Fatal("conflicting continuation executed")
	}
	if _, err = m.StopTask(t.Context(), v.ID); err != nil || remote.stops != 1 {
		t.Fatal("stop did not use original route", err)
	}
}

func TestRemoteUnknownKeepsIDAndTargetWithoutFallbackOrRedispatch(t *testing.T) {
	m, local, remote, _, router, target := routedFixture(t)
	remote.unknownStart = true
	in := input("unknown-target-request")
	in.Target = &target
	v, err := m.StartTask(t.Context(), in)
	if err == nil || v.Outcome != "unknown" {
		t.Fatal(v, err)
	}
	if err = router.Set(nodes.Node{ID: target.NodeID, Label: "Linux A"}, nodes.Capability{Target: target, State: nodes.Unavailable}, nil); err != nil {
		t.Fatal(err)
	}
	again, err := m.StartTask(t.Context(), in)
	if err != nil || again.ID != v.ID || remote.starts != 1 {
		t.Fatal("unknown start replayed", again, err)
	}
	in.Target = nil
	if _, err = m.StartTask(t.Context(), in); err == nil || local.starts != 0 {
		t.Fatal("request changed target")
	}
	if _, err = m.ReadTask(t.Context(), v.ID); err == nil || local.starts != 0 {
		t.Fatal("disconnected task rerouted")
	}
}

func TestRemoteAdmissionAndWorkspaceFailuresNeverStartWork(t *testing.T) {
	for _, mode := range []string{"source", "workspace", "persist"} {
		t.Run(mode, func(t *testing.T) {
			m, _, remote, source, _, target := routedFixture(t)
			switch mode {
			case "source":
				source.err = errors.New("no native activation")
			case "workspace":
				remote.prepareErr = errors.New("target denied path")
			case "persist":
				m.write = func() error { return errors.New("disk full") }
			}
			in := input("failed-target-request")
			in.Target = &target
			if _, err := m.StartTask(t.Context(), in); err == nil || remote.starts != 0 {
				t.Fatal("failed admission dispatched")
			}
			if mode != "workspace" && remote.prepares != 0 {
				t.Fatal("mutated target before admission/persistence")
			}
		})
	}
}

func TestLegacyLedgerMigratesAsLocalWithoutChangingReceipts(t *testing.T) {
	root := t.TempDir()
	f := newRuntime()
	in := input("old-request-binding")
	id := "task-" + hash(in.RequestID)
	old := state{Version: 1, Records: map[string]*record{id: {Provider: "codex", Fingerprint: hash(in.Title, in.Prompt), Execution: "native-turn", ReportID: "old-report", ReportState: "delivered", View: api.Task{ID: id, Workspace: "/old/work", Status: "completed"}}}}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(root, "tasks.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	m := openFixture(t, root, "codex", f)
	v, err := m.StartTask(t.Context(), in)
	if err != nil || v.ID != id || f.starts != 0 || v.Target == nil || *v.Target != localTarget("codex") {
		t.Fatal(v, err)
	}
	b, _ = os.ReadFile(m.path)
	var saved state
	if json.Unmarshal(b, &saved) != nil {
		t.Fatal("ledger not written")
	}
	r := saved.Records[id]
	if r.Target != localTarget("codex") || r.Execution != "native-turn" || r.ReportID != "old-report" || r.ReportState != "delivered" || len(r.RequestDigest) != 64 {
		t.Fatal(r)
	}
}

func TestNativeTargetOrWorkspaceDriftCannotOverwriteLedger(t *testing.T) {
	for _, mode := range []string{"target", "workspace"} {
		t.Run(mode, func(t *testing.T) {
			m, _, remote, _, _, target := routedFixture(t)
			in := input("drift-target-request")
			in.Target = &target
			v, err := m.StartTask(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			state := remote.states[v.ID]
			if mode == "target" {
				state.Target = localTarget("caelis")
			} else {
				state.Task.Workspace = "/different/project"
			}
			remote.states[v.ID] = state
			if err = m.RefreshWatchlist(); err == nil {
				t.Fatal("drift accepted")
			}
			if !m.OwnsWorkTarget(v.ID, target) {
				t.Fatal("ledger target changed")
			}
		})
	}
}
