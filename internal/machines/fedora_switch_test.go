package machines

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Two real native owners on ONE machine profile, across default switches and a
// controller restart. Uses a disposable private target; never edits user models.
func TestFedoraDefaultSwitch(t *testing.T) {
	host := os.Getenv("CAELIS_BOT_TEST_SSH")
	if host == "" {
		t.Skip("set CAELIS_BOT_TEST_SSH for native acceptance")
	}
	helper := os.Getenv("CAELIS_BOT_REMOTE_HELPER_DIR")
	s, e := Open(t.TempDir(), inert{}, func(arch string) ([]byte, error) { return os.ReadFile(filepath.Join(helper, "linux-"+arch)) })
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	input := api.MachineInput{Address: host, SSHConfig: true, Authentication: "agent", Port: 22, Name: "Worker default acceptance"}
	node, e := s.ConnectMachine(ctx, input)
	if e != nil || node.State != "trust" {
		t.Fatal("trust preparation", node.State, node.Issue, e)
	}
	input.ID, input.TrustFingerprint = node.ID, node.Fingerprint
	node, e = s.ConnectMachine(ctx, input)
	if e != nil || node.State == "offline" {
		t.Fatal("SSH connection", node.State, node.Issue, e)
	}
	bindings := map[string]api.TerminalTarget{}
	ids := map[string]string{}
	for _, runtime := range []string{"codex", "caelis"} {
		node, e = s.InspectMachine(ctx, node.ID, runtime)
		if e != nil || node.State != "ready" {
			t.Fatal("default switch", runtime, node.State, node.Issue, e)
		}
		sum := sha256.Sum256([]byte(runtime + time.Now().String()))
		id := "task-" + hex.EncodeToString(sum[:16])
		in := api.TaskStart{Machine: node.ID, RequestID: "switch-" + runtime + "-user-request", Title: "Default switch acceptance", Prompt: "Run exactly one shell command: sleep 6; printf 'WORKER_SWITCH_OK\\n' > switch.txt. Do not create other files. Reply done."}
		workspace, e := s.PrepareRemoteWork(ctx, in, id)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.StartWork(ctx, api.WorkStart{TaskStart: in, ID: id, Workspace: workspace, Instructions: "Perform the user's one-command acceptance precisely."}); e != nil {
			t.Fatal("start", runtime, e)
		}
		bindings[runtime], e = s.WorkTerminal(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		ids[runtime] = id
	}
	originalRoot := s.root
	artifact := s.artifact
	reopened, e := Open(originalRoot, inert{}, artifact)
	if e != nil {
		t.Fatal(e)
	}
	s = reopened
	wait := func(id, want string) api.Task {
		t.Helper()
		for {
			v, e := s.ReadWork(ctx, id)
			if e != nil {
				t.Fatal("read original", e)
			}
			if v.Status == want {
				return v
			}
			if v.Status == "failed" || v.Status == "awaiting_approval" || v.Status == "waiting_approval" {
				t.Fatal("unexpected state", v.Status)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(time.Second):
			}
		}
	}
	for runtime, id := range ids {
		v := wait(id, "completed")
		s.mu.Lock()
		bytes, e := s.command(ctx, s.state.Profiles[node.ID], "cat "+quote(filepath.Join(v.Workspace, "switch.txt")), nil)
		s.mu.Unlock()
		if e != nil || string(bytes) != "WORKER_SWITCH_OK\n" {
			t.Fatal("exact native artifact readback", runtime, e)
		}
		native, e := s.WorkTerminal(ctx, id)
		if e != nil || native.Runtime != runtime || native.Thread != bindings[runtime].Thread || native.Session != bindings[runtime].Session || native.Endpoint != bindings[runtime].Endpoint {
			t.Fatal("original binding replaced", runtime, e)
		}
	}
	// Continue the old Codex task while Caelis is still the new-task default.
	if _, e = s.SendWork(ctx, api.TaskMessage{ID: ids["codex"], RequestID: "continue-old-codex", Prompt: "Run exactly one shell command: sleep 30. Do not write files. Reply after it finishes."}); e != nil {
		t.Fatal("old continuation", e)
	}
	wait(ids["codex"], "working")
	// Exercise a real failed SSH connection from this disposable controller,
	// without changing user config, the Fedora network or its native owners.
	original := s.state.Profiles[node.ID]
	unreachable := original
	unreachable.View.SSHConfig = false
	// Use a separate disposable control socket so OpenSSH cannot reuse the
	// healthy Fedora multiplexed connection for this failure check.
	unreachable.View.ID += "-unreachable"
	unreachable.View.Address, unreachable.View.Port = "127.0.0.1", 9
	s.state.Profiles[node.ID] = unreachable
	failedCtx, stopFailedRead := context.WithTimeout(ctx, 5*time.Second)
	_, readErr := s.ReadWork(failedCtx, ids["codex"])
	stopFailedRead()
	s.state.Profiles[node.ID] = original
	if readErr == nil {
		t.Fatal("unreachable SSH target unexpectedly succeeded")
	}
	if s.state.Runtimes[ids["codex"]] != "codex" {
		t.Fatal("SSH failure rebound the old task")
	}
	wait(ids["codex"], "working")
	if _, e = s.StopWork(ctx, ids["codex"]); e != nil {
		t.Fatal("old stop", e)
	}
	wait(ids["codex"], "interrupted")
	if _, e = s.InspectMachine(ctx, node.ID, "codex"); e != nil {
		t.Fatal("reverse switch", e)
	}
	if _, e = s.SendWork(ctx, api.TaskMessage{ID: ids["caelis"], RequestID: "continue-old-caelis", Prompt: "Read switch.txt and reply with its exact content. Do not write files or start new work."}); e != nil {
		t.Fatal("Caelis continuation under Codex default", e)
	}
	wait(ids["caelis"], "completed")
	if len(s.WorkStates()) != 2 {
		t.Fatalf("lost owned task cache: %d", len(s.WorkStates()))
	}
	for runtime, id := range ids {
		native, e := s.WorkTerminal(ctx, id)
		if e != nil || native.Thread != bindings[runtime].Thread || native.Session != bindings[runtime].Session {
			t.Fatal("continuation replaced original", runtime, e)
		}
	}
	t.Log("same machine: Codex/Caelis default switches, exact bytes, restart, SSH failure/recovery, old-task continuation/interrupt and original terminal bindings passed")
}
