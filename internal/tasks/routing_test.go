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
	previews := m.TaskPreviews()
	if len(previews) != 1 || previews[0].Provider != "caelis" || previews[0].Target == nil || *previews[0].Target != target || previews[0].TargetLabel != "Linux A" {
		t.Fatal("preview lost execution target", previews)
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

func TestRemoteRestartPreservesSourceAndContinuationBinding(t *testing.T) {
	m, local, remote, source, router, target := routedFixture(t)
	in := input("restart-target-request")
	in.Target = &target
	v, err := m.StartTask(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	message := api.TaskMessage{ID: v.ID, RequestID: "restart-continuation", Prompt: "Continue original assignment"}
	if _, err = m.SendTask(t.Context(), message); err != nil {
		t.Fatal(err)
	}
	original := remote.lastMessage
	source.source.OperationID = "new-resident-turn"
	m, err = OpenRouted(m.path, m.root, "codex", local, local, local.Snapshot, router, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.SendTask(t.Context(), message); err != nil || remote.sends != 1 || remote.lastMessage.Source != original.Source || remote.lastMessage.RequestDigest != original.RequestDigest {
		t.Fatal("restart changed continuation receipt", err)
	}
	if again, err := m.StartTask(t.Context(), in); err != nil || again.ID != v.ID || remote.starts != 1 {
		t.Fatal("restart dispatched duplicate start", again, err)
	}
	other := input("other-target-request")
	other.Target = &target
	otherTask, err := m.StartTask(t.Context(), other)
	if err != nil {
		t.Fatal(err)
	}
	message.ID = otherTask.ID
	if _, err = m.SendTask(t.Context(), message); err == nil || remote.sends != 1 {
		t.Fatal("continuation request moved to another task")
	}
}

func TestPersistedRequestDigestRejectsChangedTargetOrSource(t *testing.T) {
	for _, mode := range []string{"target", "source", "message"} {
		t.Run(mode, func(t *testing.T) {
			m, local, _, source, router, target := routedFixture(t)
			in := input("digest-target-request")
			in.Target = &target
			v, err := m.StartTask(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "digest-continuation", Prompt: "Continue"}); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(m.path)
			if err != nil {
				t.Fatal(err)
			}
			var saved state
			if err = json.Unmarshal(b, &saved); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "target":
				saved.Records[v.ID].Target.NodeID = "other-node"
				saved.Records[v.ID].View.Target = targetPointer(saved.Records[v.ID].Target)
			case "source":
				saved.Records[v.ID].Source.OperationID = "different-turn"
			case "message":
				message := saved.Messages["digest-continuation"]
				message.Source.OperationID = "different-turn"
				saved.Messages["digest-continuation"] = message
			}
			b, _ = json.Marshal(saved)
			if err = os.WriteFile(m.path, b, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = OpenRouted(m.path, m.root, "codex", local, local, local.Snapshot, router, source); err == nil {
				t.Fatal("changed durable binding accepted")
			}
		})
	}
}

func TestManagedBotDefaultAndExplicitMacWorkerKeepExactBindings(t *testing.T) {
	root := t.TempDir()
	owned := newRuntime()
	mac := &remoteFixture{fixtureRuntime: newRuntime(), ledger: filepath.Join(root, "tasks.json"), requests: map[string]api.TaskMessage{}}
	router, err := nodes.NewAt(nodes.Node{ID: "managed-linux", Label: "Linux"}, "codex", owned)
	if err != nil {
		t.Fatal(err)
	}
	target := api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleWorker}
	if err := router.Set(nodes.Node{ID: api.LocalNodeID, Label: "Mac"}, nodes.Capability{Target: target, State: nodes.Ready}, mac); err != nil {
		t.Fatal(err)
	}
	source := &sourceFixture{source: api.WorkDispatchSource{NodeID: "managed-linux", Backend: "codex", Kind: "native_activation", BindingID: "original-resident", OperationID: "original-turn"}}
	m, err := OpenRouted(mac.ledger, filepath.Join(root, "Tasks"), "codex", owned, owned, owned.Snapshot, router, source)
	if err != nil {
		t.Fatal(err)
	}
	v, err := m.StartTask(t.Context(), input("managed-default-request"))
	if err != nil || v.Target == nil || *v.Target != router.DefaultTarget() || owned.starts != 1 || mac.starts != 0 || source.calls != 1 || owned.lastStart.Source != source.source {
		t.Fatal(v, err)
	}
	if _, err := os.Stat(v.Workspace); err != nil {
		t.Fatal("owned machine did not allocate direct workspace", err)
	}
	in := input("explicit-mac-request")
	in.Target = &target
	remote, err := m.StartTask(t.Context(), in)
	if err != nil || remote.Target == nil || *remote.Target != target || mac.starts != 1 || mac.prepares != 1 || mac.lastStart.Source != source.source {
		t.Fatal(remote, mac.lastStart, err)
	}
	if filepath.Dir(remote.Workspace) == m.root {
		t.Fatal("Mac workspace allocated on managed machine")
	}
	if got := m.DefaultWorkerTarget(); got != router.DefaultTarget() {
		t.Fatal("native default port changed", got)
	}
	if got := m.WorkRoutes(); len(got) != 2 {
		t.Fatal(got)
	}
	if _, err := OpenRouted(mac.ledger, m.root, "codex", owned, owned, owned.Snapshot, router, source); err != nil {
		t.Fatal("actual source rejected during recovery", err)
	}
	message := api.TaskMessage{ID: remote.ID, RequestID: "managed-mac-continue", Prompt: "Continue"}
	if _, err := m.SendTask(t.Context(), message); err != nil {
		t.Fatal(err)
	}
	source.source.OperationID = "later-turn"
	if _, err := m.SendTask(t.Context(), message); err != nil || mac.sends != 1 || mac.lastMessage.Source.OperationID != "original-turn" {
		t.Fatal("continuation receipt changed", mac.lastMessage, err)
	}
	if err := router.Set(nodes.Node{ID: api.LocalNodeID, Label: "Mac"}, nodes.Capability{Target: target, State: nodes.Unavailable}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadTask(t.Context(), remote.ID); err == nil {
		t.Fatal("missing original Mac route fell back")
	}
	if _, err := m.StartTask(t.Context(), in); err != nil || mac.starts != 1 || owned.starts != 1 {
		t.Fatal("original request redispatched", err)
	}
	// A ledger with a coherent digest but another source machine still fails.
	r := m.state.Records[remote.ID]
	r.Source.NodeID = api.LocalNodeID
	r.RequestDigest = requestDigest(remote.ID, r.Target, r.View.Workspace, r.Fingerprint, r.Source)
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRouted(mac.ledger, m.root, "codex", owned, owned, owned.Snapshot, router, source); err == nil {
		t.Fatal("recovery accepted a different source machine")
	}
}

func (f *remoteFixture) WorkMessageRecorded(in api.TaskMessage) bool {
	old, ok := f.requests[in.RequestID]
	return ok && old == in
}
func TestRemoteContinuationAtCapacityReconcilesPersistedAuthorization(t *testing.T) {
	m, _, remote, source, _, target := routedFixture(t)
	m.ConfigureLimit(func() int { return 2 })
	in := input("original-capacity-task")
	in.Target = &target
	task, err := m.StartTask(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	message := api.TaskMessage{ID: task.ID, RequestID: "original-continuation", Prompt: "Continue"}
	if _, err = m.SendTask(t.Context(), message); err != nil {
		t.Fatal(err)
	}
	original := remote.lastMessage
	remote.complete(task.ID)
	for _, request := range []string{"busy-first", "busy-second"} {
		in.RequestID = request
		if _, err = m.StartTask(t.Context(), in); err != nil {
			t.Fatal(err)
		}
	}
	calls := source.calls
	source.err = api.ErrWorkSourceInactive
	if _, err = m.SendTask(t.Context(), message); err != nil || remote.sends != 1 || remote.lastMessage != original || source.calls != calls {
		t.Fatal("original reconciliation required new turn or capacity", err)
	}
	message.RequestID = "new-continuation"
	if _, err = m.SendTask(t.Context(), message); err == nil {
		t.Fatal("new continuation ignored capacity")
	}
}
