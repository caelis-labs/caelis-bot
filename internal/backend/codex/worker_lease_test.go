//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type leaseReaderFixture struct {
	mu    sync.Mutex
	lease nodeplane.Lease
	err   error
	calls int
}

func (r *leaseReaderFixture) ReadWorkerLease(ctx context.Context, ref nodeplane.WorkLeaseRef) (nodeplane.Lease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.lease, r.err
}
func leaseFixtureGrant() api.WorkerLeaseGrant {
	return api.WorkerLeaseGrant{BotID: "bot", BrokerNodeID: "broker", SourceNodeID: "host", Backend: "codex", Epoch: "epoch-1"}
}
func TestLeasedWorkerActualProcessStopsBeforeQueuedAdmission(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binary, pidFile := filepath.Join(dir, "codex"), filepath.Join(dir, "pid")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	body := "#!/bin/sh\nexec " + quote(executable) + " -test.run='^TestWorkerProcessHelper$' -- " + quote(pidFile) + " \"$@\"\n"
	if err = os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	watchdog, _, _, _ := supervisorFixture(t)
	grant := leaseFixtureGrant()
	source := &workerSourceFixture{value: api.WorkDispatchSource{NodeID: "host", Backend: "codex", BindingID: "binding", OperationID: "op", Kind: "user", Lease: grant}}
	reader := &leaseReaderFixture{lease: nodeplane.Lease{BotID: grant.BotID, NodeID: grant.SourceNodeID, Backend: api.NodeCodex, Epoch: grant.Epoch, TTLMs: 60000}}
	var suspend func()
	w := NewWorker(WorkerOptions{Target: api.WorkTarget{NodeID: "worker", Backend: "codex", Role: api.RoleWorker}, Directory: filepath.Join(dir, "worker"), Binary: binary, Source: source, Lease: &WorkerLeaseOptions{HelperPath: watchdog, BrokerNodeID: "broker", BotID: "bot", SourceNode: "host", SourceBackend: "codex", Reader: reader, BindPower: func(ctx context.Context, s, w func()) (func(), error) { suspend = s; return func() {}, nil }}})
	defer w.Close(testContext(t))
	if err = w.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if !w.LeaseAwareAdmission() {
		t.Fatal("actual owned fence not negotiated")
	}
	if err = w.WorkAdmission(testContext(t)); err != nil {
		t.Fatal(err)
	}
	source.mu.Lock()
	source.value.Lease.BrokerNodeID = "unpaired"
	source.mu.Unlock()
	if w.WorkAdmission(testContext(t)) == nil {
		t.Fatal("caller changed trusted broker")
	}
	source.mu.Lock()
	source.value.Lease = grant
	source.mu.Unlock()
	pidBytes, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(string(pidBytes))
	other := exec.Command("/bin/sleep", "60")
	if err = other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Process.Kill(); _ = other.Wait() }()
	// Hold the ordinary native operation queue. Native sleep loss must bypass it.
	taskID := "task-00000000000000000000000000000001"
	workspace, err := w.ResolveWorkWorkspace(testContext(t), taskID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = w.PrepareWorkWorkspace(testContext(t), taskID, workspace, false); err != nil {
		t.Fatal(err)
	}
	target := w.target
	source.mu.Lock()
	attested := source.value
	source.mu.Unlock()
	intent := api.WorkStart{TaskStart: api.TaskStart{RequestID: "queued-start", Title: "Fixture", Prompt: "Synthetic queued effect", Target: &target}, ID: taskID, Workspace: workspace, Instructions: "Fixture worker policy", Source: attested, RequestDigest: strings.Repeat("a", 64)}
	w.engine.op.Lock()
	queued := make(chan struct{})
	result := make(chan error, 1)
	go func() { close(queued); _, err := w.StartWork(testContext(t), intent); result <- err }()
	<-queued
	started := time.Now()
	suspend()
	w.engine.op.Unlock()
	if err = <-result; err == nil {
		t.Fatal("queued effect admitted after power loss")
	}
	w.engine.mu.Lock()
	intents := len(w.engine.binding.Tasks)
	w.engine.mu.Unlock()
	if intents != 0 {
		t.Fatal("queued effect persisted or dispatched after lease loss")
	}

	if time.Since(started) >= 4*time.Second {
		t.Fatal("hard stop waited for ordinary operation queue")
	}
	if err = syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("native owned process alive", err)
	}
	if err = syscall.Kill(other.Process.Pid, 0); err != nil {
		t.Fatal("unrelated process targeted", err)
	}
	if w.LeaseAwareAdmission() || w.WorkAdmission(testContext(t)) == nil {
		t.Fatal("old generation admitted after loss")
	}
	source.mu.Lock()
	source.value.Lease.Epoch = "epoch-2"
	source.mu.Unlock()
	if w.WorkAdmission(testContext(t)) == nil {
		t.Fatal("revoked process rebound to new epoch")
	}
}
func TestWorkerLeaseDeadlineAndOriginalReceiptRecovery(t *testing.T) {
	d := workerPair(t)
	d.startUnknown = true
	in, view, err := d.start(t, 92)
	if err == nil || view.Outcome != "unknown" {
		t.Fatal("unknown fixture intent missing", view, err)
	}
	grant := api.WorkerLeaseGrant{BotID: "bot", BrokerNodeID: "broker", SourceNodeID: "host-node", Backend: "codex", Epoch: "epoch"}
	reader := &leaseReaderFixture{lease: nodeplane.Lease{BotID: "bot", NodeID: "host-node", Backend: api.NodeCodex, Epoch: "epoch", TTLMs: 60000}}
	d.w.lease = newWorkerLeaseFence(d.w, WorkerLeaseOptions{BrokerNodeID: "broker", BotID: "bot", SourceNode: "host-node", SourceBackend: "codex", Reader: reader, BindPower: func(context.Context, func(), func()) (func(), error) { return func() {}, nil }})
	f := d.w.lease
	f.enabled = true
	f.renew = func(context.Context, string, time.Time) error { return nil }
	if err = f.check(testContext(t), grant); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	deadline := f.deadline
	f.mu.Unlock()
	if remaining := time.Until(deadline); remaining > 45*time.Second || remaining < 44*time.Second {
		t.Fatal("remaining TTL not conservatively measured", remaining)
	}
	f.mu.Lock()
	f.deadline = time.Now().Add(-time.Millisecond)
	f.mu.Unlock()
	go f.maintain()
	select {
	case <-f.life.Done():
	case <-time.After(time.Second):
		t.Fatal("expired grant retained native admission")
	}
	recovered, recoveryErr := d.w.StartWork(testContext(t), in)
	if recoveryErr != nil || recovered.ID != view.ID || recovered.Outcome != "unknown" {
		t.Fatal("original retained receipt became unavailable", recovered, recoveryErr)
	}
	d.f.mu.Lock()
	starts := d.starts
	d.f.mu.Unlock()
	if starts != 1 {
		t.Fatal("unknown original intent redispatched", starts)
	}
}
