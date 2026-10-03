package machines

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
	"os"
	"path/filepath"
	"testing"
)

func TestSwitchDefaultRetainsEveryOriginalNativeRoute(t *testing.T) {
	for _, status := range []string{"working", "completed", "waiting_approval", "unknown", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			s, _ := Open(t.TempDir(), inert{}, nil)
			s.state.Profiles["machine-owned"] = profile{View: api.Machine{ID: "machine-owned", Runtime: "codex", State: "ready"}}
			tasks := map[string]map[string]api.Task{"codex": {}, "caelis": {}}
			starts := 0
			s.request = func(_ context.Context, p profile, r remotework.Request) (remotework.Response, error) {
				runtime := r.Runtime
				if runtime == "" {
					runtime = p.View.Runtime
				}
				out := remotework.Response{}
				switch r.Action {
				case "inspect":
					out.Setup.State = "ready"
				case "prepare":
					if s.state.Runtimes[r.ID] != runtime {
						t.Fatal("prepared before durable backend binding")
					}
					out.Task.Workspace = "/target/" + runtime + "/" + r.ID
				case "start":
					if s.state.Runtimes[r.Start.ID] != runtime {
						t.Fatal("dispatched before durable backend binding")
					}
					if old, ok := tasks[runtime][r.Start.ID]; ok {
						out.Task = old
						break
					}
					starts++
					out.Task = api.Task{ID: r.Start.ID, Status: status, Workspace: r.Start.Workspace}
					tasks[runtime][out.Task.ID] = out.Task
				case "read", "send", "stop", "terminal":
					var ok bool
					out.Task, ok = tasks[runtime][r.ID]
					if !ok {
						return out, errors.New("wrong original owner")
					}
					out.Terminal = api.TerminalTarget{Runtime: runtime, Thread: runtime + "-original-" + r.ID}
				case "states":
					for _, task := range tasks[runtime] {
						out.States = append(out.States, api.WorkState{Task: task})
					}
				}
				return out, nil
			}
			in := api.TaskStart{Machine: "machine-owned", RequestID: "original-user-request", Title: "old", Prompt: "old"}
			path, e := s.PrepareRemoteWork(t.Context(), in, "task-old")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.StartWork(t.Context(), api.WorkStart{TaskStart: in, ID: "task-old", Workspace: path}); e != nil {
				t.Fatal(e)
			}
			before, _ := s.WorkTerminal(t.Context(), "task-old")
			if _, e = s.InspectMachine(t.Context(), in.Machine, "caelis"); e != nil {
				t.Fatal("task blocked default switch", e)
			}
			if _, e = s.StartWork(t.Context(), api.WorkStart{TaskStart: in, ID: "task-old", Workspace: path}); e != nil {
				t.Fatal("replay original binding", e)
			}
			p := s.state.Profiles[in.Machine]
			p.View.State = "setup"
			s.state.Profiles[in.Machine] = p
			if path, e = s.PrepareRemoteWork(t.Context(), in, "task-old"); e != nil || path != "/target/codex/task-old" {
				t.Fatal("unconfigured new default blocked original preparation", e)
			}
			p.View.State = "ready"
			s.state.Profiles[in.Machine] = p
			in.RequestID = "new-user-request"
			path, e = s.PrepareRemoteWork(t.Context(), in, "task-new")
			if e != nil || path != "/target/caelis/task-new" {
				t.Fatal("new default not selected", e)
			}
			if _, e = s.StartWork(t.Context(), api.WorkStart{TaskStart: in, ID: "task-new", Workspace: path}); e != nil {
				t.Fatal(e)
			}
			again, e := Open(s.root, inert{}, nil)
			if e != nil {
				t.Fatal(e)
			}
			again.request = s.request
			s = again
			if _, e = s.ReadWork(t.Context(), "task-old"); e != nil {
				t.Fatal(e)
			}
			if _, e = s.SendWork(t.Context(), api.TaskMessage{ID: "task-old", RequestID: "continue-original"}); e != nil {
				t.Fatal(e)
			}
			if _, e = s.StopWork(t.Context(), "task-old"); e != nil {
				t.Fatal(e)
			}
			after, e := s.WorkTerminal(t.Context(), "task-old")
			if e != nil || before.Thread != after.Thread || after.Runtime != "codex" {
				t.Fatal("terminal retargeted", e)
			}
			s.mu.Lock()
			for _, runtime := range []string{"codex", "caelis"} {
				s.refreshCacheLocked(t.Context(), in.Machine, s.state.Profiles[in.Machine], runtime)
			}
			s.mu.Unlock()
			if len(s.WorkStates()) != 2 || starts != 2 {
				t.Fatal("old cache disappeared or duplicated dispatch")
			}
			if _, e = s.InspectMachine(t.Context(), in.Machine, "codex"); e != nil {
				t.Fatal("reverse switch", e)
			}
			if _, e = s.ReadWork(t.Context(), "task-new"); e != nil {
				t.Fatal("reverse switch retargeted newer task", e)
			}
			if e = s.RemoveMachine(t.Context(), in.Machine); e == nil {
				t.Fatal("removed task SSH owner")
			}
		})
	}
}
func TestLegacyRuntimeMigrationIsDurableBeforeSelectionChanges(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "machines.json"), []byte(`{"version":1,"profileId":"original","profiles":{"machine-original":{"view":{"runtime":"codex"}}},"routes":{"task-original":"machine-original"}}`), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := Open(root, inert{}, nil)
	if e != nil || s.state.Runtimes["task-original"] != "codex" {
		t.Fatal(e)
	}
	s.state.Profiles["machine-original"] = profile{View: api.Machine{Runtime: "caelis"}}
	if e = s.save(); e != nil {
		t.Fatal(e)
	}
	again, e := Open(root, inert{}, nil)
	if e != nil || again.state.Runtimes["task-original"] != "codex" {
		t.Fatal("migration re-resolved default", e)
	}
}
func TestBindingFailureNeverLeavesAnUnpersistedRoute(t *testing.T) {
	s, _ := Open(t.TempDir(), inert{}, nil)
	s.state.Profiles["machine-owned"] = profile{View: api.Machine{Runtime: "codex", State: "ready"}}
	s.root = filepath.Join(s.root, "unwritable")
	if e := os.WriteFile(s.root, []byte("file"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.BindWork(t.Context(), api.TaskStart{Machine: "machine-owned"}, "task-original", ""); e == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if len(s.state.Routes) != 0 || len(s.state.Runtimes) != 0 {
		t.Fatal("failed binding retained in memory")
	}
}

func TestDefaultSaveFailureKeepsPreviousSelection(t *testing.T) {
	s, _ := Open(t.TempDir(), inert{}, nil)
	s.state.Profiles["machine-owned"] = profile{View: api.Machine{Runtime: "codex", State: "ready"}}
	s.request = func(context.Context, profile, remotework.Request) (remotework.Response, error) {
		out := remotework.Response{}
		out.Setup.State = "ready"
		return out, nil
	}
	s.root = filepath.Join(s.root, "unwritable")
	if err := os.WriteFile(s.root, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InspectMachine(t.Context(), "machine-owned", "caelis"); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if s.state.Profiles["machine-owned"].View.Runtime != "codex" {
		t.Fatal("failed save changed the default")
	}
}
