package codex

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/roaming"
)

type leasedProcessOwner struct{ session *Session }

func (o leasedProcessOwner) WithdrawWorkerGrants(context.Context) error { return nil }
func (o leasedProcessOwner) Stop(ctx context.Context) error             { return o.session.FenceStop(ctx) }
func (o leasedProcessOwner) SafeIdle(context.Context) error             { return nil }

func TestLeaseFenceStopsOwnedNativeProcessAndDescendant(t *testing.T) {
	unrelated := exec.Command("/bin/sleep", "60")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
	binary, pid := fixtureBinary(t, "owned-tool")
	client, err := Start(testContext(t), Options{Binary: binary, CLIOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session := NewSession(SessionOptions{ForceOwned: true, Directory: t.TempDir(), Execution: api.ExecutionSettings{ApprovalMode: "auto"}})
	session.client = client
	guard := roaming.NewGuard("fixture-node", api.NodeCodex, leasedProcessOwner{session}, true)
	lease := nodeplane.Lease{BotID: "fixture-bot", NodeID: "fixture-node", Backend: api.NodeCodex, Epoch: "fixture-lease", TTLMs: 60000}
	if err = guard.Install(lease, time.Now()); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	guard.Suspend()
	if err = guard.WaitStopped(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 4*time.Second {
		t.Fatalf("owned native fence exceeded suspend budget: %s", elapsed)
	}
	assertReaped(t, pid)
	data, err := os.ReadFile(pid + "-tool")
	if err != nil {
		t.Fatal(err)
	}
	child, _ := strconv.Atoi(string(data))
	owner := client.rpc.conn.(*pipeConnection).tools
	if sameLiveProcess(child, owner.children[child]) {
		t.Fatal("leased native descendant remained alive")
	}
	if err = unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("unrelated process was affected", err)
	}
	if _, _, err = guard.Begin(t.Context()); err == nil {
		t.Fatal("suspended owner admitted new work")
	}
}
