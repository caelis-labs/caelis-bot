package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type terminalFixture struct {
	*fixtureRuntime
	opened string
	target *api.TerminalTarget
	err    error
}

func (f *terminalFixture) WorkTerminal(_ context.Context, id string) (api.TerminalTarget, error) {
	f.opened = id
	if f.err != nil {
		return api.TerminalTarget{}, f.err
	}
	if f.target != nil {
		return *f.target, nil
	}
	return api.TerminalTarget{Generation: "fixture-generation", Locality: api.TerminalLocal, Runtime: "codex", Binary: "/native/codex", Endpoint: "unix:///private/native.sock", Directory: f.states[id].Task.Workspace, Thread: "native-owned"}, nil
}

func TestTerminalUsesExplicitNativeLocalityForOpaqueLocalNode(t *testing.T) {
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	router, e := nodes.NewAt(nodes.Node{ID: "opaque-machine-identity", Label: "This Mac"}, "codex", f)
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	m, e := OpenRouted(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot, router, nil)
	if e != nil {
		t.Fatal(e)
	}
	v, e := m.StartTask(t.Context(), input("opaque-local-terminal"))
	if e != nil {
		t.Fatal(e)
	}
	target, e := m.WorkTerminal(t.Context(), v.ID)
	if e != nil || target.Target.NodeID != "opaque-machine-identity" || target.Target != m.nativeTarget || target.Directory != v.Workspace || target.Locality != api.TerminalLocal {
		t.Fatal("opaque local node lost actual locality", target, e)
	}
	if _, e = taskterminal.Script(target); e != nil {
		t.Fatal("local opaque node could not attach", e)
	}
	valid := target
	for _, mutate := range []func(*api.TerminalTarget){
		func(t *api.TerminalTarget) { t.Runtime = "caelis" },
		func(t *api.TerminalTarget) { t.Directory = filepath.Join(root, "foreign-directory") },
		func(t *api.TerminalTarget) { t.Target.NodeID = "foreign-node" },
		func(t *api.TerminalTarget) { t.Target.Role = api.RoleBot },
		func(t *api.TerminalTarget) { t.Locality = "" },
		func(t *api.TerminalTarget) { t.Generation = "" },
		func(t *api.TerminalTarget) { t.Locality = api.TerminalRemote },
	} {
		changed := valid
		mutate(&changed)
		f.target = &changed
		if got, e := m.WorkTerminal(t.Context(), v.ID); e == nil || got != (api.TerminalTarget{}) {
			t.Fatal("changed terminal binding accepted", got, e)
		}
	}
	f.target = nil
	f.err = errors.New("native task authentication unavailable")
	if _, e = m.WorkTerminal(t.Context(), v.ID); !errors.Is(e, f.err) {
		t.Fatal("native authentication failure hidden", e)
	}
}

func TestRemoteWorkerTerminalUnavailableKeepsOriginalRouteAndWorkspace(t *testing.T) {
	for _, name := range []string{"direct-ssh-codex", "direct-ssh-caelis", "registered-agent", "outgoing-broker"} {
		t.Run(name, func(t *testing.T) {
			m, local, remote, _, router, target := routedFixture(t)
			if strings.HasSuffix(name, "codex") {
				if e := router.Set(nodes.Node{ID: target.NodeID, Label: "Original Worker"}, nodes.Capability{Target: target, State: nodes.Unavailable}, nil); e != nil {
					t.Fatal(e)
				}
				target.Backend = "codex"
				if e := router.Set(nodes.Node{ID: target.NodeID, Label: "Original Worker"}, nodes.Capability{Target: target, State: nodes.Ready}, remote); e != nil {
					t.Fatal(e)
				}
			}
			in := input("original-terminal-route")
			in.Target = &target
			in.Workspace = "/target-only/original-workspace"
			v, e := m.StartTask(t.Context(), in)
			if e != nil {
				t.Fatal(e)
			}
			var port api.WorkRuntime = &workerwire.Client{}
			if name == "direct-ssh-caelis" {
				port = caelis.NewWorker(caelis.WorkerOptions{})
			}
			if e = router.Set(nodes.Node{ID: target.NodeID, Label: "Original Worker"}, nodes.Capability{Target: target, State: nodes.Ready}, port); e != nil {
				t.Fatal(e)
			}
			if _, e = m.WorkTerminal(t.Context(), v.ID); !errors.Is(e, api.ErrRemoteWorkTerminal) {
				t.Fatal("missing remote TTY channel not explicit", e)
			}
			if local.starts != 0 || remote.starts != 1 || remote.sends != 0 || remote.stops != 0 || !m.OwnsWorkTarget(v.ID, target) || m.state.Records[v.ID].View.Workspace != in.Workspace {
				t.Fatal("terminal request migrated or replayed work")
			}
			if e = router.Set(nodes.Node{ID: target.NodeID, Label: "Original Worker"}, nodes.Capability{Target: target, State: nodes.Unavailable}, nil); e != nil {
				t.Fatal(e)
			}
			if _, e = m.WorkTerminal(t.Context(), v.ID); e == nil || errors.Is(e, api.ErrRemoteWorkTerminal) {
				t.Fatal("offline node confused with supported route", e)
			}
			if local.starts != 0 || remote.starts != 1 {
				t.Fatal("offline original target fell back locally")
			}
		})
	}
}

func TestTaskPreviewPersistsOriginalPromptAndFencesOwnership(t *testing.T) {
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	path := filepath.Join(root, "tasks.json")
	m, err := Open(path, filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	in := input("original-task-prompt")
	in.Prompt = "Original prompt\nwith exact whitespace and <markup>"
	v, err := m.StartTask(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.TaskPreviews()) != 1 {
		t.Fatal("new task missing automatic pin")
	}
	if _, err = m.PinTask(v.ID, true); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	p := reopened.TaskPreviews()
	if len(p) != 1 || p[0].ID != v.ID || p[0].Prompt != in.Prompt {
		t.Fatal("original assignment changed", p)
	}
	if p[0].Status != "working" {
		t.Fatal("active preview lost native status", p)
	}
	f.complete(v.ID)
	if p = reopened.TaskPreviews(); p[0].Status != "completed" {
		t.Fatal("preview waited for ledger refresh", p)
	}
	if _, err = reopened.WorkTerminal(t.Context(), "foreign"); err == nil || f.opened != "" {
		t.Fatal("foreign task acquired")
	}
	if target, err := reopened.WorkTerminal(t.Context(), v.ID); err != nil || target.Thread != "native-owned" || f.opened != v.ID {
		t.Fatal("owned task unavailable", err)
	}
	other, err := Open(path, filepath.Join(root, "Tasks"), "other", newRuntime(), f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.TaskPreviews()) != 0 {
		t.Fatal("unsupported runtime exposed bubbles")
	}
}
