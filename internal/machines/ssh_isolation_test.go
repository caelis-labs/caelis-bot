package machines

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
)

type sshIsolationLocal struct {
	inert
	stopped chan struct{}
	once    sync.Once
}

func (l *sshIsolationLocal) StopWork(ctx context.Context, id string) (api.Task, error) {
	l.once.Do(func() { close(l.stopped) })
	return api.Task{ID: id, Status: "stopped"}, nil
}
func TestSlowSSHSettingsDoNotHoldLocalOrOtherPeerControl(t *testing.T) {
	for _, action := range []string{"inspect", "model", "configuration", "tui", "team"} {
		t.Run(action, func(t *testing.T) {
			local := &sshIsolationLocal{stopped: make(chan struct{})}
			s, e := Open(t.TempDir(), local, nil)
			if e != nil {
				t.Fatal(e)
			}
			s.state.Profiles["ssh-B"] = profile{View: api.Machine{ID: "ssh-B", Runtime: "codex", State: "ready"}}
			s.state.Profiles["ssh-C"] = profile{View: api.Machine{ID: "ssh-C", Runtime: "codex", State: "ready"}}
			s.state.Routes["other-task"] = "ssh-C"
			s.state.Runtimes["other-task"] = "codex"
			entered, release := make(chan struct{}), make(chan struct{})
			other := make(chan struct{}, 1)
			s.request = func(ctx context.Context, p profile, in remotework.Request) (remotework.Response, error) {
				if p.View.ID == "ssh-B" && in.Action == action {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return remotework.Response{}, ctx.Err()
					}
				}
				if p.View.ID == "ssh-C" && in.Action == "stop" {
					other <- struct{}{}
				}
				return remotework.Response{Setup: api.SetupState{State: "ready"}, Task: api.Task{ID: in.ID, Status: "working"}}, nil
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				switch action {
				case "inspect":
					s.InspectMachine(t.Context(), "ssh-B", "codex")
				case "model":
					s.SaveMachineModel(t.Context(), api.MachineModel{ID: "ssh-B"})
				case "configuration":
					s.ReadMachineAdvanced(t.Context(), "ssh-B")
				case "tui":
					s.MachineTerminal(t.Context(), "ssh-B")
				case "team":
					s.ChangeMachineTeam(t.Context(), api.MachineTeamChange{ID: "ssh-B"})
				}
			}()
			<-entered
			ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
			defer cancel()
			controls := make(chan struct{})
			go func() {
				defer close(controls)
				s.StopWorkRequest(ctx, api.WorkStop{ID: "local-task", RequestID: "original-local-stop"})
				s.StopWorkRequest(ctx, api.WorkStop{ID: "other-task", RequestID: "original-other-stop"})
			}()
			blocked := false
			select {
			case <-controls:
			case <-ctx.Done():
				blocked = true
			}
			close(release)
			<-done
			<-controls
			if blocked {
				t.Fatal("SSH settings network retained global route lock")
			}
			select {
			case <-local.stopped:
			default:
				t.Fatal("local stop did not reach native")
			}
			select {
			case <-other:
			default:
				t.Fatal("other peer stop did not reach native")
			}
		})
	}
}
func TestLateSSHObservationCannotOverwriteEditedOrRemovedConnection(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "edited", true: "removed"}[remove], func(t *testing.T) {
			s, _ := Open(t.TempDir(), inert{}, nil)
			s.state.Profiles["B"] = profile{View: api.Machine{ID: "B", Name: "old", Runtime: "codex", State: "offline"}}
			entered, release := make(chan struct{}), make(chan struct{})
			s.request = func(context.Context, profile, remotework.Request) (remotework.Response, error) {
				close(entered)
				<-release
				return remotework.Response{Setup: api.SetupState{State: "ready"}}, nil
			}
			done := make(chan error, 1)
			go func() { _, e := s.InspectMachine(t.Context(), "B", "codex"); done <- e }()
			<-entered
			s.mu.Lock()
			if remove {
				delete(s.state.Profiles, "B")
			} else {
				p := s.state.Profiles["B"]
				p.View.Name = "new"
				s.state.Profiles["B"] = p
			}
			s.mu.Unlock()
			close(release)
			if e := <-done; e == nil {
				t.Fatal("late old observation certified a different connection")
			}
			p, exists := s.state.Profiles["B"]
			if exists == remove || exists && (p.View.Name != "new" || p.View.State != "offline") {
				t.Fatal("late observation replaced newer configuration")
			}
		})
	}
}
func TestSSHObservationDeadlineAndFailureStayLocal(t *testing.T) {
	s, _ := Open(t.TempDir(), inert{}, nil)
	s.state.Profiles["B"] = profile{View: api.Machine{ID: "B", Runtime: "codex"}}
	s.request = func(ctx context.Context, _ profile, _ remotework.Request) (remotework.Response, error) {
		d, ok := ctx.Deadline()
		if !ok || time.Until(d) > 8*time.Second {
			t.Error("health probe not bounded")
		}
		return remotework.Response{}, errors.New("controlled offline")
	}
	v, e := s.InspectMachine(t.Context(), "B", "codex")
	if e != nil || v.State != "offline" || v.CheckedAt == 0 {
		t.Fatal(v, e)
	}
	if len(s.WorkStates()) != 0 {
		t.Fatal("failed health probe manufactured work")
	}
}

func TestMalformedSSHResponseStaysWithinExistingReadLimit(t *testing.T) {
	var output sshOutput
	chunk := make([]byte, 1<<20)
	for range 20 {
		if n, e := output.Write(chunk); e != nil || n != len(chunk) {
			t.Fatal("transport drain failed", n, e)
		}
	}
	if !output.overflow || len(output.data) != 8<<20 {
		t.Fatal("remote bytes exceeded retained response budget", len(output.data))
	}
}
