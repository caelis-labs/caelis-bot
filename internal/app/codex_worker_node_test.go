package app

import (
	"context"
	"errors"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// This native-port fixture covers APP assembly and route ownership; it never
// calls a model or claims that static fixture authority is a live activation.
type nodePrimarySource struct {
	*testEngine
	value api.WorkDispatchSource
}

func (p *nodePrimarySource) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	return p.value, nil
}

type wireNodeFixture struct {
	mu                    sync.Mutex
	pair                  workerwire.Pair
	root                  string
	revision              uint64
	changed               chan struct{}
	tasks                 map[string]api.Task
	intents               map[string]api.WorkStart
	starts, stops, closes int
}

func (w *wireNodeFixture) WorkerPair() workerwire.Pair {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pair
}
func (w *wireNodeFixture) Connect(context.Context) error { return nil }
func (w *wireNodeFixture) Close(context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closes++
	return nil
}
func (w *wireNodeFixture) Snapshot() api.Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return api.Snapshot{Connection: "ready", Revision: w.revision}
}
func (w *wireNodeFixture) WaitSnapshot(ctx context.Context, revision uint64) (api.Snapshot, error) {
	for {
		w.mu.Lock()
		current, changed := w.revision, w.changed
		w.mu.Unlock()
		if current > revision {
			return w.Snapshot(), nil
		}
		select {
		case <-ctx.Done():
			return api.Snapshot{}, ctx.Err()
		case <-changed:
		}
	}
}
func (w *wireNodeFixture) WorkAdmission(ctx context.Context) error {
	_, err := workerwire.SourceProvider().WorkDispatchSource(ctx)
	return err
}
func (w *wireNodeFixture) WorkStates() []api.WorkState {
	w.mu.Lock()
	defer w.mu.Unlock()
	var states []api.WorkState
	for _, v := range w.tasks {
		target := *v.Target
		v.Target = &target
		states = append(states, api.WorkState{Target: w.pair.Target, Task: v})
	}
	return states
}
func (w *wireNodeFixture) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if previous, exists := w.intents[in.ID]; exists {
		if previous.Source != in.Source || previous.RequestDigest != in.RequestDigest || previous.Prompt != in.Prompt {
			return w.tasks[in.ID], errors.New("original intent changed")
		}
		return w.tasks[in.ID], nil
	}
	actual, err := workerwire.SourceProvider().WorkDispatchSource(ctx)
	if err != nil {
		return api.Task{}, err
	}
	if actual != in.Source {
		return api.Task{}, errors.New("foreign source changed")
	}
	target := w.pair.Target
	v := api.Task{ID: in.ID, Target: &target, Title: in.Title, Workspace: in.Workspace, Status: "working", Outcome: "accepted"}
	w.tasks[in.ID] = v
	w.intents[in.ID] = in
	w.starts++
	w.revision++
	close(w.changed)
	w.changed = make(chan struct{})
	return v, nil
}
func (w *wireNodeFixture) ReadWork(_ context.Context, id string) (api.Task, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	v, ok := w.tasks[id]
	if !ok {
		return v, errors.New("not owned")
	}
	return v, nil
}
func (w *wireNodeFixture) SendWork(context.Context, api.TaskMessage) (api.Task, error) {
	return api.Task{}, errors.New("unused fixture mutation")
}
func (w *wireNodeFixture) StopWork(_ context.Context, id string) (api.Task, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	v, ok := w.tasks[id]
	if !ok {
		return v, errors.New("not owned")
	}
	w.stops++
	return v, nil
}
func (w *wireNodeFixture) ResolveWorkWorkspace(_ context.Context, id, requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	return path.Join(w.root, id), nil
}
func (w *wireNodeFixture) PrepareWorkWorkspace(_ context.Context, _ string, workspace string, _ bool) error {
	return os.Mkdir(workspace, 0700)
}
func (w *wireNodeFixture) WorkApprovals() []api.WorkApproval { return nil }
func (w *wireNodeFixture) DecideWork(context.Context, api.WorkApproval, api.Decision) error {
	return errors.New("unused fixture decision")
}

func TestCodexWorkerAssemblyUsesActualBotIdentitySourceAndExactRoute(t *testing.T) {
	root := t.TempDir()
	primary := &nodePrimarySource{testEngine: newTestEngine(), value: api.WorkDispatchSource{NodeID: api.LocalNodeID, Backend: "fixture", BindingID: "native-fixture-thread", OperationID: "native-fixture-turn", Kind: "native_activation"}}
	resident, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resident.Close()
	a := &Application{root: root, engine: primary, companion: resident}
	registry, err := nodes.New("fixture", primary)
	if err != nil {
		t.Fatal(err)
	}
	remoteRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := backend.WorkerNodeConfig{ID: "codex-node", Label: "Codex fixture", Backend: "codex", SSH: "fixture", Socket: "/private/native-worker.sock", WorkspaceRoot: remoteRoot}
	remote := &wireNodeFixture{root: remoteRoot, revision: 1, changed: make(chan struct{}), tasks: map[string]api.Task{}, intents: map[string]api.WorkStart{}}
	controller := openWorkerNodes(filepath.Join(root, "worker-nodes.json"), registry, func(c backend.WorkerNodeConfig, directory string) (workerNodeAdapter, error) {
		adapter, err := a.newWorkerNodeAdapter(c, directory)
		if err != nil {
			return nil, err
		}
		native, ok := adapter.(*codexWorkerNodeAdapter)
		if !ok {
			t.Fatal("explicit Codex route enrolled Caelis")
		}
		pair := native.config.Pair
		if pair.BotID != productrpc.ProfileBotID(resident.State().ID) || pair.SourceBackend != "fixture" || pair.SourceNode != api.LocalNodeID || native.config.Source != primary {
			t.Fatal("native product/source identity replaced", pair)
		}
		remote.mu.Lock()
		if remote.pair.BotID == "" {
			remote.pair = pair
		} else if remote.pair != pair {
			t.Error("reconnect changed native pairing")
		}
		remote.mu.Unlock()
		native.connect = func(ctx context.Context, c nodes.CodexSSHConfig) (*nodes.CodexSSHWorker, error) {
			owner := nodeworker.New(remote)
			server, err := workerwire.NewServer(owner, c.Pair)
			if err != nil {
				return nil, err
			}
			left, right := net.Pipe()
			go func() { _ = server.Serve(t.Context(), right) }()
			client, err := workerwire.NewClient(ctx, c.Pair, c.Source, left)
			if err != nil {
				return nil, err
			}
			return &nodes.CodexSSHWorker{Client: client}, nil
		}
		return native, nil
	})
	defer controller.Close()
	saved, err := controller.Save(config, controller.Snapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := controller.Connect(t.Context(), config.ID, saved.Revision)
	if err != nil || ready.Nodes[0].State != "ready" {
		t.Fatal(ready, err)
	}
	manager, err := tasks.OpenRouted(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "fixture", primary, primary, primary.Snapshot, registry, primary)
	if err != nil {
		t.Fatal(err)
	}
	target := configuredWorkerTarget(config)
	in := api.TaskStart{RequestID: "app-native-start", Title: "APP wire fixture", Prompt: "Isolated synthetic result", Target: &target}
	work, err := manager.StartTask(t.Context(), in)
	if err != nil || work.Target == nil || *work.Target != target || !strings.HasPrefix(work.Workspace, remoteRoot+"/") {
		t.Fatal("APP did not delegate exact target", work, err)
	}
	runtime, err := registry.WorkRuntimeFor(target)
	if err != nil {
		t.Fatal(err)
	}
	runtime.(*nodes.CodexSSHWorker).Close() // Simulate observation stream loss only.
	reconnected, err := controller.Connect(t.Context(), config.ID, ready.Revision)
	if err != nil || reconnected.Nodes[0].State != "ready" {
		t.Fatal("explicit reconnect retained a dead observer", reconnected, err)
	}
	ready = reconnected
	detached, err := controller.Disconnect(t.Context(), config.ID, ready.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if remote.stops != 0 || remote.closes != 0 {
		t.Fatal("APP observer detach stopped native owner")
	}
	if _, err = registry.WorkRuntimeFor(target); err == nil {
		t.Fatal("detached route remained ready")
	}
	if _, err = controller.Connect(t.Context(), config.ID, detached.Revision); err != nil {
		t.Fatal(err)
	}
	if original, err := manager.StartTask(t.Context(), in); err != nil || original.ID != work.ID {
		t.Fatal("reconnect moved original task", original, err)
	}
	remote.mu.Lock()
	starts := remote.starts
	remote.mu.Unlock()
	if starts != 1 {
		t.Fatal("APP replay dispatched a second worker", starts)
	}
	if _, err = registry.ResolveWorkTarget(nil); err != nil {
		t.Fatal("Codex route blocked direct local default", err)
	}
}
