package weixin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

func TestNewWeixinInputDoesNotDrainHistoricalReplies(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "new", Phase: "completed", Items: []api.Item{
		{ID: "old-user", Kind: "user", RequestID: "old-input", TurnKey: "old"},
		{ID: "old-final", Kind: "assistant", Text: "unrelated old result", Status: "completed", TurnKey: "old"},
		{ID: "new-user", Kind: "user", RequestID: "new-input", TurnKey: "new"},
		{ID: "new-final", Kind: "assistant", Text: "latest result", Status: "completed", TurnKey: "new"},
	}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(snapshot) }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "owner"
	freshWindow(b, "old-input")
	b.state.Inputs["old-input"] = "accepted"
	b.ObserveTurn(api.Snapshot{CurrentTurn: "old", Phase: "completed", Items: snapshot.Items})
	b.state.Window.Used = windowSendLimit
	b.queueProgress(api.Item{ID: "old-queued", TurnKey: "old", Text: "unrelated old progress"})
	freshWindow(b, "new-input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	for range 3 {
		b.output(t.Context(), p)
	}
	if len(*texts) != 1 || (*texts)[0] != "latest result" || b.state.Window.Used != 1 || len(b.state.Progress) != 0 {
		t.Fatalf("old history borrowed new allowance: sends=%q used=%d queue=%+v", *texts, b.state.Window.Used, b.state.Progress)
	}
}

func TestDesktopInputStopsWeixinBacklog(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "wx", Phase: "completed", Items: []api.Item{
		{ID: "wx-user", Kind: "user", RequestID: "wx-input", TurnKey: "wx"},
		{ID: "old-final", Kind: "assistant", Text: "old result", Status: "completed", TurnKey: "wx"},
		{ID: "desktop-user", Kind: "user", RequestID: "desktop-input", TurnKey: "desktop"},
		{ID: "late-wx-final", Kind: "assistant", Text: "late old output", Status: "completed", TurnKey: "wx"},
	}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(snapshot) }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "owner"
	freshWindow(b, "wx-input")
	b.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	if len(*texts) != 0 || b.state.Window.Used != 0 {
		t.Fatalf("desktop boundary leaked: %s", strings.Join(*texts, "|"))
	}
}

// All three inputs may steer the same native Turn. Its earlier mixed origin
// must neither leak old text nor suppress the response to the latest input.
func TestWeixinDesktopWeixinSameTurnAndInternalEvents(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "working", Items: []api.Item{
		{ID: "first", Kind: "user", RequestID: "wx-1", TurnKey: "turn", SeenAt: 1},
		{ID: "stream", Kind: "assistant", Text: "old streaming text", Status: "inProgress", TurnKey: "turn", SeenAt: 2},
	}}
	host := Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(snapshot) }}
	root := t.TempDir()
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "owner"
	freshWindow(b, "wx-1")
	b.state.Inputs["wx-1"] = "accepted"
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.output(t.Context(), p)
	snapshot.Items = append(snapshot.Items, api.Item{ID: "desktop", Kind: "user", RequestID: "desktop", TurnKey: "turn", SeenAt: 3, Text: "PRIVATE internal worker notice"})
	snapshot.Items[1].Status = "completed"
	b.output(t.Context(), p)
	if len(*texts) != 0 {
		t.Fatal("real user with notice-like text did not close route", *texts)
	}
	snapshot.Items = append(snapshot.Items,
		api.Item{ID: "second", Kind: "user", RequestID: "wx-2", TurnKey: "turn", SeenAt: 4},
		api.Item{ID: "report", Kind: "hostNotice", RequestID: "report", TurnKey: "turn", SeenAt: 5},
		api.Item{ID: "wake", Kind: "activation", RequestID: "wake", TurnKey: "turn", SeenAt: 6},
		api.Item{ID: "latest", Kind: "assistant", Text: "latest same-turn result", Status: "completed", TurnKey: "turn", SeenAt: 7},
	)
	freshWindow(b, "wx-2")
	snapshot.Phase = "completed"
	for range 2 {
		b.output(t.Context(), p)
		b, err = Open(root, host)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(*texts) != 1 || (*texts)[0] != "latest same-turn result" || b.state.Window.Used != 1 {
		t.Fatalf("mixed-turn route: %q %+v", *texts, b.state.Window)
	}
	// Paging a stale echo with its original IM timestamp cannot reset the boundary.
	snapshot.Items = append(snapshot.Items, api.Item{ID: "older-echo", Kind: "user", RequestID: "wx-1", TurnKey: "turn", SeenAt: 1})
	b.output(t.Context(), p)
	if len(*texts) != 1 {
		t.Fatal("history load replayed output", *texts)
	}
}

func TestInFlightChunkKeepsOutcomeAndStopsRemainingChunks(t *testing.T) {
	for _, outcome := range []struct{ name, response, state string }{
		{"accepted", `{"ret":0,"message_id":"original-server-id"}`, "accepted"},
		{"rejected", `{"ret":-2,"errmsg":"rate limited"}`, "rejected"},
		{"unknown", `invalid-json`, "unknown"},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			var current atomic.Pointer[api.Snapshot]
			old := api.Snapshot{Connection: "ready", CurrentTurn: "old", Phase: "completed", Items: []api.Item{
				{ID: "old-user", Kind: "user", RequestID: "old", TurnKey: "old"},
				{ID: "long", Kind: "assistant", Text: strings.Repeat("a", 2400) + "\n\n" + strings.Repeat("b", 2400), Status: "completed", TurnKey: "old"},
			}}
			current.Store(&old)
			host := Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(*current.Load()) }}
			root := t.TempDir()
			b, err := Open(root, host)
			if err != nil {
				t.Fatal(err)
			}
			b.state.OwnerID = "owner"
			freshWindow(b, "old")
			next := api.Snapshot{Connection: "ready", CurrentTurn: "new", Phase: "completed", Items: append(append([]api.Item(nil), old.Items...),
				api.Item{ID: "new-user", Kind: "user", RequestID: "new", TurnKey: "new"},
				api.Item{ID: "new-answer", Kind: "assistant", Text: "fresh answer", Status: "completed", TurnKey: "new"},
			)}
			calls := 0
			server, texts := sentTexts(t, func(string) string {
				calls++
				if calls == 1 {
					current.Store(&next)
					b.mu.Lock()
					freshWindow(b, "new")
					b.mu.Unlock()
					return outcome.response
				}
				return `{"ret":0}`
			})
			defer server.Close()
			p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
			b.output(t.Context(), p)
			receipt := b.state.Outputs["item:long:0"]
			if len(*texts) != 1 || receipt.State != outcome.state || receipt.Attempts != 1 || b.state.Window.Used != 0 || b.state.Window.Exhausted {
				t.Fatalf("in-flight outcome %q receipt=%+v window=%+v", *texts, receipt, b.state.Window)
			}
			b.output(t.Context(), p)
			b, err = Open(root, host)
			if err != nil {
				t.Fatal(err)
			}
			b.output(t.Context(), p)
			if len(*texts) != 2 || (*texts)[1] != "fresh answer" || b.state.Window.Used != 1 || b.state.Outputs["item:long:1"].Attempts != 0 || b.state.Outputs["item:long:0"].ClientID != receipt.ClientID {
				t.Fatalf("late chunks replayed: %q %+v", *texts, b.state.Window)
			}
		})
	}
}

func TestRestartExpiresLegacyQueueAndPreservesUnknownReceipts(t *testing.T) {
	snapshot := api.Snapshot{Connection: "offline", Phase: "working", CurrentTurn: "new", Items: []api.Item{
		{ID: "new-user", Kind: "user", RequestID: "new", TurnKey: "new"},
	}}
	host := Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(snapshot) }}
	root := t.TempDir()
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "owner"
	freshWindow(b, "new")
	b.state.Progress = []pendingProgress{{ID: "legacy", TurnKey: "old", Text: "old legacy progress"}}
	b.state.PendingFinal = "old-final"
	b.state.FinalProgressFor, b.state.FinalProgressIDs = "old-final", []string{"legacy"}
	b.state.ProgressBatch = []string{"legacy"}
	b.state.Outputs["pending-file"] = outbound{State: "preparing", FileInputID: "old", ArtifactID: "old-artifact"}
	b.state.Outputs["original-unknown"] = outbound{State: "unknown", Attempts: 1, ClientID: "original-client-id"}
	if err := b.saveLocked(); err != nil {
		t.Fatal(err)
	}
	b, err = Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.output(t.Context(), nil)
	if len(b.state.Progress) != 0 || b.state.PendingFinal != "" || len(b.state.ProgressBatch) != 0 || b.state.Window.Used != 0 || b.state.Outputs["pending-file"].State != "superseded" || b.state.Outputs["original-unknown"].ClientID != "original-client-id" {
		t.Fatalf("legacy queue survived: %+v", b.state)
	}
	snapshot.Connection, snapshot.Phase = "ready", "completed"
	snapshot.Items = append(snapshot.Items, api.Item{ID: "answer", Kind: "assistant", Text: "current reply", Status: "completed", TurnKey: "new"})
	server, texts := sentTexts(t, nil)
	defer server.Close()
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.output(t.Context(), p)
	b, err = Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.output(t.Context(), p)
	if len(*texts) != 1 || (*texts)[0] != "current reply" || b.state.Window.Used != 1 {
		t.Fatal("reconnect replayed or lost current reply", *texts)
	}
}

func TestDesktopBoundaryDuringFilePreparationDoesNotSpendBudget(t *testing.T) {
	f := newFileFixture(t)
	f.uploadGate = make(chan struct{})
	var current atomic.Pointer[api.Snapshot]
	initial := fileSnapshot([]api.Artifact{{ID: "safe", Name: "safe.txt"}}, "")
	current.Store(&initial)
	root := t.TempDir()
	path := filepath.Join(root, "safe.txt")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(root, Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(*current.Load()) }, Artifact: func(string) (string, error) { return path, nil }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "owner"
	freshWindow(b, "input")
	b.output(t.Context(), f.protocol())
	// Resolver/upload may already be in flight. Change the real user boundary
	// before releasing CDN preparation; sendmessage must never be attempted.
	next := initial
	next.Items = append(append([]api.Item(nil), initial.Items...), api.Item{ID: "desktop", Kind: "user", RequestID: "desktop", TurnKey: "desktop"})
	current.Store(&next)
	close(f.uploadGate)
	out := waitFile(t, b, "safe")
	if out.State != "superseded" || out.Attempts != 0 || b.state.Window.Used != 0 {
		t.Fatalf("old file consumed allowance: %+v", out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.items) != 0 {
		t.Fatal("old file reached sendmessage")
	}
}

func TestReadyFinalPrecedesProgressAndNeverDrainsItAfterRestart(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "completed", Items: []api.Item{
		{ID: "user", Kind: "user", RequestID: "input", TurnKey: "turn"},
		{ID: "progress", Kind: "assistant", Status: "completed", TurnKey: "turn", Text: strings.Repeat("progress", 500)},
		{ID: "final", Kind: "assistant", Status: "completed", TurnKey: "turn", Text: strings.Repeat("final", 900)},
	}}
	host := Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(snapshot) }}
	root := t.TempDir()
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "owner"
	freshWindow(b, "input")
	b.state.Window.Used = 8
	b.queueProgress(snapshot.Items[1])
	for range 3 {
		b.output(t.Context(), p)
		b, err = Open(root, host)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(*texts) != 2 || strings.Contains(strings.Join(*texts, ""), "progress") || strings.Contains(strings.Join(*texts, ""), longReplyNotice) || b.state.Window.Used != 10 || len(b.state.Progress) != 0 {
		t.Fatalf("latest full result lost to progress: %q %+v", *texts, b.state.Window)
	}
}

func TestIngressWithoutContextFencesOldWindowBeforeDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"next","msgs":[{"message_id":"new","from_user_id":"owner","to_user_id":"bot","message_type":1,"item_list":[{"type":1,"text_item":{"text":"new input"}}]}]}`))
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	recorded := make(chan textchannel.Inbound, 1)
	b, err := Open(t.TempDir(), Host{RecordInput: func(in textchannel.Inbound, secret bool) { recorded <- in }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID, b.state.BotID = "owner", "bot"
	freshWindow(b, "old")
	b.state.Window.Used = 8
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.receive(ctx, &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	}()
	select {
	case in := <-recorded:
		if in.ID != "weixin:bot:new" || in.Text != "new input" || in.Channel != "weixin" {
			t.Errorf("wrong published ingress: %+v", in)
		}
	case <-time.After(2 * time.Second):
		t.Error("recovery-delayed dispatch hid the new user boundary")
	}
	cancel()
	<-done
	if b.state.Window.InputID != "weixin:bot:new" || b.state.Window.ContextToken != "" || b.remainingLocked(true) != 0 || len(b.state.Inbox) != 1 || b.state.Cursor != "next" {
		t.Fatalf("new input retained old authority: %+v", b.state.Window)
	}
}

func TestReconnectWaitsForBoundaryAndKeepsOnlyCurrentPendingProgress(t *testing.T) {
	snapshot := api.Snapshot{Connection: "offline"}
	host := Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(snapshot) }}
	root := t.TempDir()
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "owner"
	freshWindow(b, "input")
	progress := api.Item{ID: "progress", Kind: "assistant", TurnKey: "turn", Status: "completed", Text: "current progress"}
	b.queueProgress(progress)
	b, err = Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.output(t.Context(), nil)
	if len(b.state.Progress) != 1 || b.state.Window.Used != 0 {
		t.Fatal("restoring snapshot lost current progress")
	}
	server, texts := sentTexts(t, nil)
	defer server.Close()
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	snapshot = api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "working", Items: []api.Item{{ID: "user", Kind: "user", RequestID: "input", TurnKey: "turn"}, progress}}
	for range 2 {
		b.output(t.Context(), p)
		b, err = Open(root, host)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(*texts) != 1 || (*texts)[0] != "current progress" || b.state.Window.Used != 1 || b.state.Window.ProgressUsed != 1 {
		t.Fatal("current progress lost or replayed", *texts)
	}
}

func TestDesktopBoundaryStopsRemainingFinalChunks(t *testing.T) {
	var current atomic.Pointer[api.Snapshot]
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "completed", Items: []api.Item{
		{ID: "user", Kind: "user", RequestID: "input", TurnKey: "turn"},
		{ID: "final", Kind: "assistant", TurnKey: "turn", Status: "completed", Text: strings.Repeat("result", 700)},
	}}
	current.Store(&snapshot)
	host := Host{Snapshot: func() api.Snapshot { return nativeInputSnapshot(*current.Load()) }}
	root := t.TempDir()
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.OwnerID = "owner"
	freshWindow(b, "input")
	next := snapshot
	next.Items = append(append([]api.Item(nil), snapshot.Items...), api.Item{ID: "desktop", Kind: "user", RequestID: "desktop", TurnKey: "desktop"})
	server, texts := sentTexts(t, func(string) string { current.Store(&next); return `{"ret":0}` })
	defer server.Close()
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.output(t.Context(), p)
	b, err = Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.output(t.Context(), p)
	if len(*texts) != 1 || b.state.Window.Used != 1 || b.state.Outputs["item:final:0"].State != "accepted" || b.state.Outputs["item:final:1"].Attempts != 0 {
		t.Fatal("desktop boundary did not stop later chunk", len(*texts), b.state.Window)
	}
}
