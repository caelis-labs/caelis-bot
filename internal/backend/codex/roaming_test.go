package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/roaming"
)

func TestManagedSessionForcesOwnedProcessWhileDefaultDiscoveryIsUnchanged(t *testing.T) {
	for _, managed := range []bool{false, true} {
		directory := t.TempDir()
		s := NewSession(SessionOptions{Directory: directory, StateFile: filepath.Join(directory, "fresh-binding.json"), ForceOwned: managed, Socket: "unchanged-local-socket", Execution: api.ExecutionSettings{ApprovalMode: "auto"}})
		called := false
		s.start = func(ctx context.Context, opts Options) (*Client, error) {
			called = true
			if opts.CLIOnly != managed || opts.Attachable == managed || opts.Socket != "unchanged-local-socket" {
				t.Fatalf("wrong native ownership options: %+v", opts)
			}
			return nil, errors.New("owned fixture startup denied")
		}
		err := s.Connect(t.Context())
		if managed && !OwnedRuntimeSupported() {
			if called || !errors.Is(err, ErrOwnedRuntimeUnsupported) {
				t.Fatal("unsupported managed ownership reached native process admission", called, err)
			}
			continue
		}
		if !called {
			t.Fatal("did not reach native process admission")
		}
	}
}

type queuedLeaseAdmission struct {
	guard    *roaming.Guard
	admitted chan struct{}
}

func (q queuedLeaseAdmission) Begin(ctx context.Context) (context.Context, func(), error) {
	ctx, release, err := q.guard.Begin(ctx)
	close(q.admitted)
	return ctx, release, err
}
func (q queuedLeaseAdmission) CheckContext(ctx context.Context) error {
	return q.guard.CheckContext(ctx)
}

type queuedNativeOwner struct{}

func (queuedNativeOwner) WithdrawWorkerGrants(context.Context) error { return nil }
func (queuedNativeOwner) Stop(context.Context) error                 { return nil }
func (queuedNativeOwner) SafeIdle(context.Context) error             { return nil }

func TestQueuedNativePromptAndTaskCannotCrossLeaseRevocation(t *testing.T) {
	for _, kind := range []string{"connect", "prompt", "task"} {
		t.Run(kind, func(t *testing.T) {
			guard := roaming.NewGuard("owned", api.NodeCodex, queuedNativeOwner{}, true)
			if err := guard.Install(nodeplane.Lease{BotID: "stable", NodeID: "owned", Backend: api.NodeCodex, Epoch: "1", TTLMs: 60000}, time.Now()); err != nil {
				t.Fatal(err)
			}
			admitted := make(chan struct{})
			dir := t.TempDir()
			s := NewSession(SessionOptions{ForceOwned: true, Directory: dir, StateFile: filepath.Join(dir, "binding.json"), Execution: api.ExecutionSettings{ApprovalMode: "auto"}, Admission: queuedLeaseAdmission{guard, admitted}})
			s.start = func(context.Context, Options) (*Client, error) {
				t.Error("fenced native runtime startup dispatched")
				return nil, errors.New("dispatched")
			}
			s.op.Lock()
			finished := make(chan error, 1)
			go func() {
				var err error
				switch kind {
				case "connect":
					err = s.Connect(t.Context())
				case "prompt":
					_, err = s.Submit(t.Context(), api.Submission{ID: "queued-prompt", Text: "Synthetic input"}, nil)
				case "task":
					_, err = s.StartWork(t.Context(), api.WorkStart{TaskStart: api.TaskStart{RequestID: "queued-task", Title: "Fixture", Prompt: "Synthetic task"}})
				}
				finished <- err
			}()
			<-admitted
			guard.Revoke()
			s.op.Unlock()
			if err := <-finished; err == nil {
				t.Fatal("queued native operation admitted after revocation")
			}
			if _, err := os.Stat(filepath.Join(dir, "binding.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("native binding created after fence", err)
			}
			if err := guard.WaitStopped(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
