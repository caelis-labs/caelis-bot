package machines

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in billable acceptance: real SSH, original native Worker, target-local
// accounts, readback and reconnect. No daily Bot profile or runtime is replaced.
func TestFedoraNativeWorkers(t *testing.T) {
	host := os.Getenv("CAELIS_BOT_TEST_SSH")
	if host == "" {
		t.Skip("set CAELIS_BOT_TEST_SSH for real remote acceptance")
	}
	helper := os.Getenv("CAELIS_BOT_REMOTE_HELPER_DIR")
	s, e := Open(t.TempDir(), inert{}, func(arch string) ([]byte, error) { return os.ReadFile(filepath.Join(helper, "linux-"+arch)) })
	if e != nil {
		t.Fatal(e)
	}
	ctx, c := context.WithTimeout(t.Context(), 4*time.Minute)
	defer c()
	in := api.MachineInput{Address: host, Port: 22, Authentication: "agent", Name: "Fedora acceptance"}
	m, e := s.ConnectMachine(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if m.State != "trust" {
		t.Fatalf("initial connection %s %s", m.State, m.Issue)
	}
	in.ID = m.ID
	in.TrustFingerprint = m.Fingerprint
	m, e = s.ConnectMachine(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if m.State == "offline" {
		t.Fatalf("SSH connection: %s", m.Issue)
	}
	for _, runtime := range []string{"codex", "caelis"} {
		t.Run(runtime, func(t *testing.T) {
			// A different machine profile keeps runtime ownership immutable.
			node := m
			if runtime == "caelis" {
				v := in
				v.ID = ""
				v.Name = "Fedora Caelis"
				v.TrustFingerprint = m.Fingerprint
				var err error
				node, err = s.ConnectMachine(ctx, v)
				if err != nil {
					t.Fatal(err)
				}
			}
			node, e = s.InspectMachine(ctx, node.ID, runtime)
			if e != nil || node.State != "ready" {
				t.Fatalf("native readiness %s %s %v", node.State, node.Issue, e)
			}
			if runtime == "codex" {
				node, e = s.SaveMachineModel(ctx, api.MachineModel{ID: node.ID, Selection: api.WorkExecutionSettings{Model: "gpt-6-luna"}})
				if e != nil {
					t.Fatal(e)
				}
			}
			sum := sha256.Sum256([]byte(runtime + node.ID + "20261003"))
			id := "task-" + hex.EncodeToString(sum[:16])
			start := api.TaskStart{Machine: node.ID, RequestID: "remote-e2e-20261003-" + runtime + "-" + node.ID, Title: "Remote E2E", Prompt: "Run exactly one shell command: sleep 12; printf 'REMOTE_E2E_OK\\n' > remote-e2e.txt. Write only that file in the current directory, read it back, and reply REMOTE_E2E_DONE."}
			workspace, e := s.PrepareRemoteWork(ctx, start, id)
			if e != nil {
				t.Fatal(e)
			}
			task, e := s.StartWork(ctx, api.WorkStart{TaskStart: start, ID: id, Workspace: workspace, Instructions: botpolicy.WorkerInstructions})
			if e != nil {
				t.Fatalf("start %s %v", task.Status, e)
			}
			var terminal api.TerminalTarget
			for {
				terminal, e = s.WorkTerminal(ctx, id)
				if e == nil {
					break
				}
				current, readErr := s.ReadWork(ctx, id)
				if readErr != nil || current.Status == "failed" {
					t.Fatal("native task binding not accepted", readErr, current.Outcome)
				}
				select {
				case <-ctx.Done():
					t.Fatal("original binding", e)
				case <-time.After(time.Second):
				}
			}
			script, e := taskterminal.Script(terminal)
			if e != nil {
				t.Fatal(e)
			}
			if len(script) == 0 || len(terminal.SSH) == 0 {
				t.Fatal("missing SSH TUI route")
			}
			// Attach/detach twice to the exact native target while the Worker runs.
			observer := fmt.Sprintf("cb-e2e-%s-%d", runtime, time.Now().UnixNano())
			native := terminal
			native.SSH = nil
			nativeScript, err := taskterminal.Script(native)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				s.mu.Lock()
				p := s.state.Profiles[node.ID]
				_, err = s.command(ctx, p, "tmux new-session -d -s "+quote(observer)+" -x 150 -y 45 "+quote(nativeScript), nil)
				s.mu.Unlock()
				if err != nil {
					t.Fatal("native attach", err)
				}
				time.Sleep(1200 * time.Millisecond)
				s.mu.Lock()
				_, err = s.command(ctx, p, "tmux kill-session -t "+quote(observer), nil)
				s.mu.Unlock()
				if err != nil {
					t.Fatal("observer disconnect", err)
				}
			}
			// Reopen the persisted controller connection; no start request is resent.
			reopened, err := Open(s.root, inert{}, func(arch string) ([]byte, error) { return os.ReadFile(filepath.Join(helper, "linux-"+arch)) })
			if err != nil {
				t.Fatal(err)
			}
			s = reopened
			var final api.Task
			for {
				final, e = s.ReadWork(ctx, id)
				if e != nil {
					t.Fatal(e)
				}
				if final.Status == "completed" {
					break
				}
				if final.Status == "failed" || final.Status == "interrupted" || final.Status == "awaiting_approval" || final.Status == "waiting_approval" {
					t.Fatalf("native state %s %s", final.Status, final.Result)
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(time.Second):
				}
			}
			s.mu.Lock()
			p := s.state.Profiles[node.ID]
			bytes, e := s.command(ctx, p, "cat "+quote(filepath.Join(workspace, "remote-e2e.txt")), nil)
			s.mu.Unlock()
			if e != nil || string(bytes) != "REMOTE_E2E_OK\n" {
				t.Fatal("exact readback failed", e)
			}
			after, e := s.WorkTerminal(ctx, id)
			if e != nil || after.Thread != terminal.Thread || after.Session != terminal.Session {
				t.Fatal("native task binding changed", e)
			}
			waitStatus := func(want string) api.Task {
				t.Helper()
				for {
					current, err := s.ReadWork(ctx, id)
					if err != nil {
						t.Fatal("read original task", err)
					}
					if current.Status == want {
						return current
					}
					if current.Status == "failed" || current.Status == "awaiting_approval" || current.Status == "waiting_approval" {
						t.Fatalf("unexpected native state %s while waiting for %s", current.Status, want)
					}
					select {
					case <-ctx.Done():
						t.Fatalf("waiting for %s: %v", want, ctx.Err())
					case <-time.After(time.Second):
					}
				}
			}
			// Continue and interrupt the original native task, then resume it again.
			// These operations must not allocate another Worker or change its target.
			_, e = s.SendWork(ctx, api.TaskMessage{ID: id, RequestID: start.RequestID + "-interrupt", Prompt: "Run exactly one shell command: sleep 30. Do not write any files. Wait for it to finish before replying."})
			if e != nil {
				t.Fatal("continue original task", e)
			}
			waitStatus("working")
			if _, e = s.StopWork(ctx, id); e != nil {
				t.Fatal("stop original task", e)
			}
			waitStatus("interrupted")
			_, e = s.SendWork(ctx, api.TaskMessage{ID: id, RequestID: start.RequestID + "-resume", Prompt: "Read remote-e2e.txt and report its exact content. Do not write files or start any new task."})
			if e != nil {
				t.Fatal("resume original task", e)
			}
			final = waitStatus("completed")
			after, e = s.WorkTerminal(ctx, id)
			if e != nil || after.Thread != terminal.Thread || after.Session != terminal.Session || after.Endpoint != terminal.Endpoint {
				t.Fatal("continuation or interrupt replaced native binding", e)
			}
			states := s.WorkStates()
			owned := 0
			for _, state := range states {
				if state.Task.Machine == node.ID {
					owned++
					if state.Task.ID != id {
						t.Fatal("allocated an unexpected Worker")
					}
				}
			}
			if owned != 1 {
				t.Fatalf("expected one original Worker, got %d", owned)
			}
			if e = s.RemoveMachine(ctx, node.ID); e == nil {
				t.Fatal("removed original task owner")
			}
			data, _ := json.Marshal(map[string]any{"runtime": runtime, "status": final.Status, "exactBytes": len(bytes), "sameNativeBinding": true, "observerDisconnects": 2, "controllerReopened": true, "continued": true, "interrupted": true, "resumed": true, "workerCount": owned, "model": node.Work.Model, "workspace": workspace, "terminal": terminal})
			if dir := os.Getenv("CAELIS_BOT_TEST_EVIDENCE"); dir != "" {
				os.MkdirAll(dir, 0700)
				os.WriteFile(filepath.Join(dir, runtime+".json"), data, 0600)
			}
			t.Log("real remote native Worker completed, exact readback, original binding and independent model passed")
		})
	}
}
