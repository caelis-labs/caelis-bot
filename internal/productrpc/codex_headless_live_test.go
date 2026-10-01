package productrpc

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type codexHeadlessFacts struct {
	Counts             map[string]int `json:"counts"`
	NodeAlive          bool           `json:"node_alive"`
	ListenerAlive      bool           `json:"listener_alive"`
	Dropped            bool           `json:"response_dropped"`
	ThreadPresent      bool           `json:"thread_present"`
	TurnPresent        bool           `json:"native_turn_present"`
	NativeChildren     int            `json:"owned_native_children"`
	ChildrenAlive      int            `json:"native_children_alive"`
	NativeMCPDispatch  bool           `json:"native_mcp_dispatch_recorded"`
	DispatchNativeTurn bool           `json:"dispatch_native_turn_present"`
	ToolAttempt        bool           `json:"tool_attempt"`
	TaskToolDiscovered bool           `json:"task_tool_discovered"`
	Tasks              []struct {
		Thread  bool   `json:"thread_present"`
		Turn    bool   `json:"turn_present"`
		Status  string `json:"status"`
		Outcome string `json:"outcome"`
	} `json:"tasks"`
}

func codexHeadlessControl(t *testing.T, ctx context.Context, f headlessFixture, action string) codexHeadlessFacts {
	t.Helper()
	command := "python3 " + quoteFixture(f.Root+"/fixture.py") + " " + quoteFixture(f.Root) + " " + quoteFixture(action)
	cmd := exec.CommandContext(ctx, "ssh", fixtureSSHArgs(f.Target, command)...)
	b, err := cmd.Output()
	if err != nil {
		t.Fatal("isolated Codex control unavailable", err)
	}
	var facts codexHeadlessFacts
	if json.Unmarshal(b, &facts) != nil {
		t.Fatal("invalid Codex control facts")
	}
	return facts
}

// Explicit metadata comes from a foreground supervisor owning a random private
// /tmp profile. Its only model provider is synthetic loopback; auth is disabled.
func TestNativeLinuxCodexHeadlessProduct(t *testing.T) {
	path := os.Getenv("CAELIS_BOT_CODEX_PRODUCT_SSH_FIXTURE")
	if path == "" {
		t.Skip("set isolated Linux Codex synthetic fixture metadata")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f headlessFixture
	if json.Unmarshal(b, &f) != nil || !f.Synthetic || f.NativeVersion != "0.159.2" || !regexp.MustCompile(`^/tmp/caelis-bot-issue47-codex-product-[a-f0-9]{16}$`).MatchString(f.Root) || !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`).MatchString(f.Target) {
		t.Fatal("unsafe Codex fixture metadata")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	c := connectHeadlessFixture(t, ctx, f)
	state := waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.Connection == "ready" })
	if state.Initialization.Required {
		r, e := c.Command(ctx, Command{ID: "native-introduction", Kind: "initialize", Introduction: &api.BotIntroduction{Name: "Synthetic Linux Codex", Description: "Temporary loopback provider only"}})
		if e != nil || r.Outcome != "accepted" {
			t.Fatal("native initialization", r.Outcome, e)
		}
	}
	waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.CanSend && s.Initialization.Status == "accepted" })
	r, err := c.Command(ctx, Command{ID: "native-lost", Kind: "submit", Submission: &api.Submission{ID: "native-lost", Text: "CASE_PRODUCT_LOST"}})
	if err == nil || r.Outcome != "unknown" {
		t.Fatal("lost response must remain unknown", r.Outcome, err)
	}
	c.Close()
	c = connectHeadlessFixture(t, ctx, f)
	r, err = c.Receipt(ctx, "native-lost")
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("original accepted receipt unavailable", r.Outcome, err)
	}
	waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.CanSend })
	facts := codexHeadlessControl(t, ctx, f, "status")
	if !facts.ThreadPresent || !facts.TurnPresent || facts.NativeChildren == 0 || facts.Counts["CASE_PRODUCT_LOST"] != 1 || !facts.Dropped {
		t.Fatal("real native thread/turn or non-replay evidence missing", facts)
	}
	r, err = c.Command(ctx, Command{ID: "native-detach", Kind: "submit", Submission: &api.Submission{ID: "native-detach", Text: "CASE_PRODUCT_DETACH"}})
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("hold admission", r.Outcome, err)
	}
	waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.CanInterrupt })
	c.Close()
	facts = codexHeadlessControl(t, ctx, f, "status")
	if !facts.NodeAlive || !facts.ListenerAlive || facts.ChildrenAlive == 0 {
		t.Fatal("detach stopped owned native process", facts)
	}
	c = connectHeadlessFixture(t, ctx, f)
	r, err = c.Receipt(ctx, "native-detach")
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("detached original receipt unavailable", r.Outcome, err)
	}
	codexHeadlessControl(t, ctx, f, "release")
	waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.CanSend })
	r, err = c.Command(ctx, Command{ID: "native-local-worker", Kind: "submit", Submission: &api.Submission{ID: "native-local-worker", Text: "CASE_PRODUCT_WORKER: Start exactly one local Worker with requestId synthetic-local-worker. Its self-contained prompt is CASE_CODEX_LOCAL_WORKER and requests only a synthetic text reply. Do not read files or call any other tool."}})
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("local Worker parent turn", r.Outcome, err)
	}
	waitHeadlessState(t, ctx, c, func(s State) bool { return s.Snapshot.CanSend })
	facts = codexHeadlessControl(t, ctx, f, "status")
	if !facts.ToolAttempt || len(facts.Tasks) != 1 || !facts.Tasks[0].Thread || !facts.Tasks[0].Turn || (!facts.NativeMCPDispatch || !facts.DispatchNativeTurn) || facts.Tasks[0].Status != "completed" || facts.Tasks[0].Outcome != "accepted" || facts.Counts["CASE_CODEX_LOCAL_WORKER"] != 1 {
		t.Fatal("genuine local Worker source/binding/completion not proven", facts)
	}
	t.Logf("local Worker attempt: discovered=%t called=%t syntheticWorkerRequests=%d", facts.TaskToolDiscovered, facts.ToolAttempt, facts.Counts["CASE_CODEX_LOCAL_WORKER"])
	r, err = c.Command(ctx, Command{ID: "native-stop", Kind: "stop-bot"})
	if err != nil || r.Outcome != "accepted" {
		t.Fatal("explicit stop receipt", r.Outcome, err)
	}
	for {
		facts = codexHeadlessControl(t, ctx, f, "status")
		if !facts.NodeAlive && !facts.ListenerAlive && facts.ChildrenAlive == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned processes/listener remain", facts)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if facts.Counts["CASE_PRODUCT_DETACH"] != 1 {
		t.Fatal("detach replayed native turn")
	}
	t.Log("real Linux Codex0.159.2 primary Bot init/thread/turn, unknown original receipt, observer detach/reconnect, explicit owner/native-child/listener stop passed; only fresh synthetic loopback provider")
}
