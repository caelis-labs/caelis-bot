//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func newRecoverableWorkerFixture(t *testing.T, path string, id ownerProcessIdentity, task bool) (*WorkOwner, SessionOptions) {
	t.Helper()
	root := t.TempDir()
	opts := SessionOptions{Directory: filepath.Join(root, "Worker"), WorkRoot: filepath.Join(root, "Tasks"), StateFile: filepath.Join(root, "binding.json"), Socket: path}
	seed := NewSession(opts)
	seed.binding.OwnerEndpoint, seed.binding.OwnerProcess = "unix://"+path, &id
	if task {
		seed.binding.Tasks = map[string]*taskRecord{
			"task-11111111111111111111111111111111": {Owner: "codex:original-private", Build: "original-build", Thread: "original-worker-thread", Run: "original-worker-turn", Pending: "original-unknown-request", View: api.Task{ID: "task-11111111111111111111111111111111", Status: "unknown", Outcome: "unknown"}, Requests: map[string]taskReceipt{"original-unknown-request": {Kind: "send", IntentFingerprint: "original-digest", Outcome: "unknown", Phase: "dispatching"}}},
		}
	}
	if err := seed.save(); err != nil {
		t.Fatal(err)
	}
	seed.cancelLife()
	w, err := NewWorkOwner(opts)
	if err != nil {
		t.Fatal(err)
	}
	w.engine.reconnectDelay = func(int) time.Duration { return time.Hour }
	t.Cleanup(func() {
		_ = w.DetachForUpdate(testContext(t))
		w.engine.cancelLife()
	})
	return w, opts
}

func TestWorkerExplicitNewWorkRecoveryFromExitedPrivateOwner(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "empty-readiness"
		if existing {
			name = "retained-unknown-work"
		}
		t.Run(name, func(t *testing.T) {
			cmd, path, id := controlledOwnerProcess(t)
			w, opts := newRecoverableWorkerFixture(t, path, id, existing)
			if err := w.Connect(testContext(t)); err != nil {
				t.Fatal("original native readiness", err)
			}
			w.engine.mu.Lock()
			original := w.engine.client
			w.engine.mu.Unlock()
			if original.UsesSharedServer() {
				t.Fatal("private owner became shared")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			if privateOwnerLifecycle(id) != ownerGone {
				t.Fatal("original controlled process did not exit")
			}
			// Process exit does not mean the asynchronous wire reader has consumed
			// EOF. Connect can still observe the existing ready client until then;
			// that is not a replacement owner. Synchronize the fixture's disconnect.
			select {
			case <-original.Done():
			case <-testContext(t).Done():
				t.Fatal("original client did not observe controlled owner exit")
			}
			// Neither a normal reconnect nor a fresh observer of the same source
			// may silently replace the original native execution owner.
			if err := w.Connect(testContext(t)); err == nil {
				t.Fatal("normal Connect replaced exited original owner")
			}
			var starts atomic.Int32
			w.engine.start = func(ctx context.Context, o Options) (*Client, error) {
				starts.Add(1)
				if o.Socket != "" || o.RequiredSocket {
					return nil, errors.New("stale private endpoint still pinned")
				}
				_, freshPath, freshID := controlledOwnerProcess(t)
				o.Socket, o.RequiredSocket = freshPath, true
				c, err := Start(ctx, o)
				if err != nil {
					return nil, err
				}
				if !c.reattachPrivateOwner(nil, "unix://"+freshPath, freshID) {
					c.detachForReconnect()
					return nil, errors.New("new controlled private process unverified")
				}
				return c, nil
			}
			if err := w.RecoverNewWork(testContext(t)); err != nil || starts.Load() != 1 {
				t.Fatal("explicit new-work recovery", starts.Load(), err)
			}
			if got, err := w.NativeEndpoint(); err != nil || got == "unix://"+path {
				t.Fatal("new actual endpoint not available", got, err)
			}
			if existing {
				old := w.engine.binding.Tasks["task-11111111111111111111111111111111"]
				if !w.engine.binding.LostOwnerTasks[old.View.ID] || old.Owner != "codex:original-private" || old.Thread != "original-worker-thread" || old.Pending != "original-unknown-request" {
					t.Fatal("old work migrated or lost original identity", old)
				}
				q := api.WorkOperationQuery{TaskID: old.View.ID, RequestID: old.Pending, Kind: "send", Fingerprint: "original-digest"}
				if op, err := w.ReadWorkOperation(testContext(t), q); err != nil || op.Outcome != "unknown" {
					t.Fatal("old uncertain effect was rewritten", op, err)
				}
				if _, err := w.SendWork(testContext(t), api.TaskMessage{ID: old.View.ID, RequestID: "cannot-migrate-original", Prompt: "Do not run"}); err == nil {
					t.Fatal("old task resumed on new owner")
				}
			}
			const nextID = "task-22222222222222222222222222222222"
			workspace := filepath.Join(opts.WorkRoot, nextID)
			if err := os.MkdirAll(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			work, err := w.StartWork(testContext(t), api.WorkStart{ID: nextID, Workspace: workspace, Instructions: "Synthetic Worker fixture", TaskStart: api.TaskStart{RequestID: "new-owner-original-create", Title: "New work", Prompt: "Synthetic native request only"}, ExecutionOverride: &api.WorkExecutionSettings{Model: "native-default"}})
			if err != nil || work.Outcome != "accepted" || work.Status != "working" {
				t.Fatal("new owner could not create independent work", work, err)
			}
			if err := w.RecoverNewWork(testContext(t)); err != nil || starts.Load() != 1 {
				t.Fatal("repeated action replaced a living new owner", starts.Load(), err)
			}
			if len(w.engine.binding.LostOwners) != 1 {
				t.Fatal("original native archive duplicated")
			}
		})
	}
}

func TestWorkerNewWorkRecoveryRetainsUnknownLifecycleAndSharedPins(t *testing.T) {
	_, path, id := controlledOwnerProcess(t)
	w, _ := newRecoverableWorkerFixture(t, path, id, true)
	var starts atomic.Int32
	w.engine.start = func(context.Context, Options) (*Client, error) {
		starts.Add(1)
		return nil, errors.New("fixture handshake failure")
	}
	w.engine.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return ownerUnknown }
	if err := w.RecoverNewWork(testContext(t)); err == nil || starts.Load() != 0 || len(w.engine.binding.LostOwners) != 0 {
		t.Fatal("unknown lifecycle authorized replacement", starts.Load(), err)
	}
	states := w.WorkStates()
	if len(states) != 1 || states[0].Activity == api.WorkActivityOwnerUnavailable {
		t.Fatal("unknown process released execution capacity", states)
	}
	w.engine.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return ownerAlive }
	if err := w.RecoverNewWork(testContext(t)); err == nil || starts.Load() != 1 || len(w.engine.binding.LostOwners) != 0 || w.engine.opts.Socket != path || !w.engine.opts.RequiredSocket {
		t.Fatal("living owner was archived or its pin cleared", starts.Load(), err)
	}
	// A shared owner has no private PID+birth binding. Its explicit standard
	// endpoint remains the only reconnect target even when unavailable.
	shared, err := NewWorkOwner(SessionOptions{Directory: t.TempDir(), Socket: path})
	if err != nil {
		t.Fatal(err)
	}
	defer shared.engine.cancelLife()
	shared.engine.start = func(_ context.Context, o Options) (*Client, error) {
		if o.Socket != path || !o.RequiredSocket {
			t.Fatal("shared endpoint pin changed")
		}
		return nil, errors.New("shared fixture unavailable")
	}
	if err := shared.RecoverNewWork(testContext(t)); err == nil || len(shared.engine.binding.LostOwners) != 0 {
		t.Fatal("shared owner became private replacement", err)
	}
}

func TestWorkerFailedNewOwnerPreparationKeepsArchivedProofAcrossReopen(t *testing.T) {
	cmd, path, id := controlledOwnerProcess(t)
	w, opts := newRecoverableWorkerFixture(t, path, id, true)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	w.engine.start = func(context.Context, Options) (*Client, error) {
		return nil, errors.New("fixture replacement temporarily unavailable")
	}
	if err := w.RecoverNewWork(testContext(t)); err == nil || len(w.engine.binding.LostOwners) != 1 {
		t.Fatal("new owner failure lost original archive", err)
	}
	reopened, err := NewWorkOwner(opts) // Host cache intentionally still has old Socket.
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.engine.cancelLife()
	if reopened.engine.opts.Socket != "" || reopened.engine.opts.RequiredSocket {
		t.Fatal("archived private socket cache locked future new work")
	}
	if old := reopened.engine.binding.Tasks["task-11111111111111111111111111111111"]; old.Requests["original-unknown-request"].IntentFingerprint != "original-digest" || old.Requests["original-unknown-request"].Outcome != "unknown" {
		t.Fatal("archived unknown identity changed")
	}
	if !strings.HasPrefix(reopened.engine.binding.LostOwners[0].Endpoint, "unix://") {
		t.Fatal("lost original endpoint proof")
	}
}
