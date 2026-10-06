package telegram

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	tg "github.com/mymmrac/telego"
)

// This opt-in test runs an owned, isolated native Codex App Server and a
// disposable MCP tool. Telegram delivery is still a local transport fixture;
// its callback is never represented as a human or live Bot API click.
func TestNativeCodexApprovalThroughTelegramCallback(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_NATIVE_APPROVAL") != "1" {
		t.Skip("opt-in native model acceptance")
	}
	cli, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	marker := filepath.Join(root, "synthetic-effect.once")
	fixture, err := filepath.Abs("testdata/native_approval_mcp.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	s := codex.NewSession(codex.SessionOptions{
		Binary: cli, Socket: filepath.Join(root, "owned-only.sock"), Directory: root,
		StateFile: filepath.Join(root, "binding.json"), RequireApproval: true,
		Execution: api.ExecutionSettings{Model: "gpt-6-luna", ApprovalMode: "ask"},
	})
	if err := s.ConfigureBotTools(&api.ToolConnection{
		Command: python, Args: []string{fixture},
		Env:          map[string]string{"BOT_APPROVAL_TEST_MARKER": marker},
		Instructions: "Use only the explicitly requested disposable MCP fixture tool. Do not access other files or services.",
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if err := s.Close(closeCtx); err != nil {
			t.Logf("owned session close: %v", err)
		}
	}()
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if snap := s.Snapshot(); snap.Connection != "ready" {
		t.Fatalf("native runtime is not ready: %s", snap.Connection)
	}
	receipt, err := s.Submit(ctx, api.Submission{ID: "native-approval-fixture-once", Text: "Call mcp__caelis_bot__fixture_approval_probe exactly once. It only records a synthetic marker in this test directory. Wait for the result, then reply PROBE_FINISHED. Do not call any other tool."}, nil)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatalf("native submit: %s %v", receipt.Outcome, err)
	}
	var approval api.Approval
	for {
		snap := s.Snapshot()
		for _, a := range snap.Approvals {
			if a.Status == "pending" {
				approval = a
				break
			}
		}
		if approval.ID != "" {
			break
		}
		if snap.Phase == "completed" || snap.Phase == "failed" || ctx.Err() != nil {
			t.Fatalf("no native approval; phase=%s connection=%s error=%v", snap.Phase, snap.Connection, ctx.Err())
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("tool ran before native approval: %v", err)
	}
	var once string
	for _, choice := range approval.Choices {
		if choice.ID == "accept" && choice.Scope == "once" {
			once = choice.ID
		}
		if choice.Scope == "conversation" || choice.Scope == "rule" {
			t.Fatalf("synthetic MCP elicitation invented persistent authority: %+v", approval.Choices)
		}
	}
	if once == "" {
		t.Fatalf("native one-call option missing: %+v", approval.Choices)
	}
	var decisions atomic.Int32
	b, client := testBridge(t, Host{
		Snapshot: s.Snapshot,
		Decide: func(ctx context.Context, decision api.Decision) error {
			decisions.Add(1)
			return s.Decide(ctx, decision)
		},
	})
	paired(b)
	b.mirror(ctx, client, s.Snapshot())
	record := b.state.Messages["approval:"+approval.ID]
	if len(record.IDs) == 0 || record.IDs[len(record.IDs)-1] <= 0 || record.Keyboard == "" {
		t.Fatalf("native approval was not presented with an actionable button: %+v", record)
	}
	if len(client.keyboards) == 0 || client.keyboards[len(client.keyboards)-1] == nil {
		t.Fatal("native approval did not reach the Telegram transport keyboard")
	}
	query := &tg.CallbackQuery{ID: "synthetic-callback-one", From: tg.User{ID: 20},
		Message: &tg.Message{MessageID: record.IDs[len(record.IDs)-1], Chat: tg.Chat{ID: 10, Type: "private"}},
		Data:    callbackID(approval, once)}
	if !b.callback(ctx, client, query) {
		t.Fatal("native decision through Bot callback was not acknowledged")
	}
	query.ID = "synthetic-callback-competing"
	if !b.callback(ctx, client, query) || decisions.Load() != 1 {
		t.Fatalf("competing callback dispatched again: %d", decisions.Load())
	}
	restored, err := Open(filepath.Dir(b.path), b.host)
	if err != nil {
		t.Fatal(err)
	}
	query.ID = "synthetic-callback-after-restart"
	if !restored.callback(ctx, client, query) || decisions.Load() != 1 {
		t.Fatalf("callback replayed after bridge restart: %d", decisions.Load())
	}
	for {
		snap := s.Snapshot()
		data, readErr := os.ReadFile(marker)
		if readErr == nil && snap.Phase == "completed" {
			if strings.TrimSpace(string(data)) != "one synthetic call" {
				t.Fatalf("unexpected synthetic effect: %q", data)
			}
			break
		}
		if readErr != nil && !os.IsNotExist(readErr) || snap.Phase == "failed" || ctx.Err() != nil {
			t.Fatalf("native approved call did not complete: %v / %s / %v", readErr, snap.Phase, ctx.Err())
		}
		time.Sleep(100 * time.Millisecond)
	}
	b.mirror(ctx, client, s.Snapshot())
	card := b.state.Messages["approval:"+approval.ID]
	if !card.Closed || card.Keyboard != "" || client.edits == 0 {
		t.Fatalf("native result did not settle the original Telegram fixture card: %+v", card)
	}
	t.Log("native App Server MCP approval -> Bot snapshot -> Telegram button fixture -> exact once decision -> resolved card -> one independent tool effect")
}
