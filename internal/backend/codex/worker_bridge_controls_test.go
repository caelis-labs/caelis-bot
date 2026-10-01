package codex

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

func TestWorkerBridgeCurrentControlsRetainNativeTurnAndApprovalIDs(t *testing.T) {
	d := workerPair(t)
	pair := workerwire.Pair{Target: d.w.target, BotID: "paired-product-bot", SourceNode: "host-node", SourceBackend: "codex"}
	d.w.pair = pair
	if err := os.Chmod(d.w.engine.opts.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := workerwire.BindPair(d.w.engine.opts.Directory, pair, false); err != nil {
		t.Fatal(err)
	}
	in, _, err := d.start(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, other, err := d.start(t, 2)
	if err != nil {
		t.Fatal(err)
	}
	d.w.source = workerwire.SourceProvider()
	owner := nodeworker.New(d.w)
	server, err := workerwire.NewServer(owner, pair)
	if err != nil {
		t.Fatal(err)
	}
	serverStream, clientStream := net.Pipe()
	ctx, cancel := context.WithCancel(testContext(t))
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, serverStream) }()
	client, err := workerwire.NewClient(testContext(t), pair, d.source, clientStream)
	if err != nil {
		cancel()
		_ = clientStream.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("control observer did not detach")
		}
	})
	s := d.w.engine
	s.mu.Lock()
	task := s.binding.Tasks[in.ID]
	thread, run := task.Thread, task.Run
	s.mu.Unlock()
	d.f.emit(wireMessage{ID: raw("worker-approval-original"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{"threadId": thread, "turnId": run, "itemId": "owned-command", "command": "synthetic", "cwd": in.Workspace, "availableDecisions": []string{"accept", "decline"}})})
	awaitState(t, s, func(api.Snapshot) bool { return len(d.w.WorkApprovals()) == 1 })
	approval := d.w.WorkApprovals()[0]
	decision := api.Decision{ID: approval.Approval.ID, Choice: approval.Approval.Choices[1].ID}
	d.source.mu.Lock()
	d.source.err = api.ErrWorkSourceInactive
	d.source.mu.Unlock()
	// Paired UI controls can act while the resident has no activation. They
	// retain original native task/approval identity instead of fabricating one.
	if err := client.DecideWork(testContext(t), approval, decision); err != nil {
		t.Fatal("idle paired approval failed", err)
	}
	select {
	case reply := <-d.f.answers:
		if string(reply.ID) != `"worker-approval-original"` || !strings.Contains(string(reply.Result), "decline") {
			t.Fatal("framed decision changed native identity", reply)
		}
	case <-testContext(t).Done():
		t.Fatal("native decision missing")
	}
	if _, err := client.StopWork(testContext(t), in.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, s, func(api.Snapshot) bool {
		v, e := d.w.ReadWork(testContext(t), in.ID)
		return e == nil && v.Status == "interrupted"
	})
	if v, err := d.w.ReadWork(testContext(t), other.ID); err != nil || v.Status != "working" {
		t.Fatal("framed cancel affected another native turn", v, err)
	}
	d.f.mu.Lock()
	interrupts := append([]map[string]string(nil), d.interrupts...)
	d.f.mu.Unlock()
	if len(interrupts) != 1 || interrupts[0]["threadId"] != thread || interrupts[0]["turnId"] != run {
		t.Fatal("framed cancel changed original native turn", interrupts)
	}
}
