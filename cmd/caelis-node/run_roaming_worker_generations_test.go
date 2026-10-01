//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type roamingEpochFixture struct {
	mu    sync.Mutex
	lease nodeplane.Lease
}

func (r *roamingEpochFixture) CurrentLease(context.Context, string) (nodeplane.Lease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lease := r.lease
	lease.ExpiresAt = time.Now().Add(time.Minute)
	return lease, nil
}
func (r *roamingEpochFixture) ReadWorkerLease(ctx context.Context, ref nodeplane.WorkLeaseRef) (nodeplane.Lease, error) {
	return r.CurrentLease(ctx, ref.BotID)
}
func (r *roamingEpochFixture) WorkDispatchSource(ctx context.Context) (api.WorkDispatchSource, error) {
	l, _ := r.CurrentLease(ctx, "")
	return api.WorkDispatchSource{NodeID: l.NodeID, Backend: string(l.Backend), BindingID: "actual-native-binding", OperationID: "actual-native-operation-" + l.Epoch, Kind: "native_activation", Lease: api.WorkerLeaseGrant{BotID: l.BotID, BrokerNodeID: "paired-broker", SourceNodeID: l.NodeID, Backend: string(l.Backend), Epoch: l.Epoch}}, nil
}
func TestRoamingWorkerEpochRolloverStopsOldOwnerAndNeverReplaysOriginalIDs(t *testing.T) {
	root := canonicalWorkerTestRoot(t)
	helper, err := verifiedRoamingExecutable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	pidfile := filepath.Join(root, "worker.pid")
	binary := filepath.Join(root, "codex")
	if err = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(helper)+" -test.run='^TestWorkerCLINativeHelper$' -- "+quote(pidfile)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(pidfile+".allow-effects", nil, 0600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "agent")
	if err = os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	c := roamingCommand{NodeID: "worker-node", BotID: "native-bot", Backend: "codex", AgentDirectory: directory, CodexBinary: binary, BrokerNodeID: "paired-broker"}
	p := roamingWorkerPlan{Sources: []roamingWorkerSource{{NodeID: "main-node", Backends: []string{"codex"}}}, Runtimes: []roamingWorkerRuntime{{Backend: "codex", Binary: binary, Execution: &api.WorkExecutionSettings{Model: "fixture-model", Effort: "medium"}}}}
	reader := &roamingEpochFixture{lease: nodeplane.Lease{BotID: c.BotID, NodeID: "main-node", Backend: api.NodeCodex, Epoch: "epoch-1", TTLMs: 60000}}
	workers := newRoamingOwnedWorkers(t.Context(), c, p, helper, reader, func(context.Context, func(), func()) (func(), error) { return func() {}, nil })
	defer func() {
		if err := workers.Close(); err != nil {
			t.Error(err)
		}
	}()
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: c.NodeID, Backend: "codex", Role: api.RoleWorker}, BotID: api.ProfileBotID(c.BotID), SourceNode: "main-node", SourceBackend: "codex"}
	open := func() (*workerwire.Client, string) {
		t.Helper()
		endpoint, e := workers.resolve(workerTestBound(t), pair)
		if e != nil {
			methods, _ := os.ReadFile(pidfile + ".methods")
			t.Fatalf("resolve %v methods=%s", e, methods)
		}
		stream, e := net.Dial("unix", endpoint.Socket)
		if e != nil {
			t.Fatal(e)
		}
		client, e := workerwire.NewClient(workerTestBound(t), pair, reader, stream)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(client.Close)
		return client, endpoint.Socket
	}
	first, socket1 := open()
	start := func(client *workerwire.Client, n string) api.WorkStart {
		t.Helper()
		id := "task-" + strings.Repeat(n, 32)
		workspace, e := client.ResolveWorkWorkspace(workerTestBound(t), id, "")
		if e != nil {
			t.Fatal(e)
		}
		if e = client.PrepareWorkWorkspace(workerTestBound(t), id, workspace, false); e != nil {
			t.Fatal(e)
		}
		source, _ := reader.WorkDispatchSource(t.Context())
		in := api.WorkStart{TaskStart: api.TaskStart{RequestID: "native-start-" + n, Title: "Contained native Worker", Prompt: "Synthetic effect", Target: &pair.Target}, ID: id, Workspace: workspace, Instructions: "Contained native policy", Source: source, RequestDigest: strings.Repeat(n, 64)}
		if task, e := client.StartWork(workerTestBound(t), in); e != nil {
			methods, _ := os.ReadFile(pidfile + ".methods")
			t.Fatalf("native start %+v %v methods=%s requests=%v", task, e, methods, workers.workers[pair].guard.book.Requests)
		}
		return in
	}
	original := start(first, "a")
	originalBook := workers.workers[pair].guard.book
	pidbytes, _ := os.ReadFile(pidfile)
	oldPID, _ := strconv.Atoi(string(pidbytes))
	reader.mu.Lock()
	reader.lease.Epoch = "epoch-2"
	reader.mu.Unlock()
	second, socket2 := open()
	if socket1 == socket2 {
		t.Fatal("source epoch reused prior native endpoint")
	}
	if e := syscall.Kill(oldPID, 0); !errors.Is(e, syscall.ESRCH) {
		t.Fatalf("old owned native process not verified stopped %v", e)
	}
	if task, e := second.StartWork(workerTestBound(t), original); e == nil || task.ID != original.ID || task.Outcome != "unknown" {
		t.Fatalf("original start was rerouted %+v %v", task, e)
	}
	current, _ := reader.WorkDispatchSource(t.Context())
	message := api.TaskMessage{ID: original.ID, RequestID: "old-send", Prompt: "Must not cross native generation", Source: current, RequestDigest: strings.Repeat("c", 64)}
	if task, e := second.SendWork(workerTestBound(t), message); e == nil || task.ID != original.ID || task.Outcome != "unknown" {
		t.Fatalf("original task send crossed generations %+v %v", task, e)
	}
	start(second, "b")
	methods, _ := os.ReadFile(pidfile + ".methods")
	if strings.Count(string(methods), "turn/start\n") != 2 {
		t.Fatalf("original IDs caused extra native task effect %s", methods)
	}
	if originalBook.epoch() != "epoch-1" {
		t.Fatal("retained original epoch rewritten")
	}
	book := workers.workers[pair].guard.book
	reopened, e := openRoamingWorkerOrigins(filepath.Dir(book.file), pair)
	if e != nil || reopened.Tasks[original.ID] == book.Generation {
		t.Fatalf("original task generation not retained on disk %v", e)
	}
	if _, e := first.ResolveWorkWorkspace(workerTestBound(t), "task-"+strings.Repeat("d", 32), ""); e == nil {
		t.Fatal("old stream gained fresh generation authority")
	}
}
func TestRoamingWorkerOriginalLedgerRequiresConfirmedStop(t *testing.T) {
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: "worker-node", Backend: "codex", Role: api.RoleWorker}, BotID: "bot", SourceNode: "main-node", SourceBackend: "codex"}
	book, e := openRoamingWorkerOrigins(t.TempDir(), pair)
	if e != nil {
		t.Fatal(e)
	}
	if e = book.begin("generation-1", "epoch-1"); e != nil {
		t.Fatal(e)
	}
	if e = book.begin("generation-2", "epoch-2"); e == nil {
		t.Fatal("unknown prior owned stop permitted new generation")
	}
	if e = book.state("generation-1", "stopped"); e != nil {
		t.Fatal(e)
	}
	if e = book.begin("generation-2", "epoch-2"); e != nil {
		t.Fatal(e)
	}
}

func TestRoamingWorkerUntrackedPreviousOwnerCannotBeReplaced(t *testing.T) {
	directory := t.TempDir()
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: "worker-node", Backend: "codex", Role: api.RoleWorker}, BotID: "bot", SourceNode: "main-node", SourceBackend: "codex"}
	if e := os.WriteFile(filepath.Join(directory, "worker-bindings.json"), []byte(`{"retained":"unknown"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := openRoamingWorkerOrigins(directory, pair); e == nil {
		t.Fatal("untracked original native receipts silently replaced")
	}
}
