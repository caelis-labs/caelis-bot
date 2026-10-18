package weixin

import (
	"context"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

func TestProgressUsesAtMostFiveSendsAndFinalMergesRemainder(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "working", Items: []api.Item{{ID: "user", Kind: "user", TurnKey: "turn", RequestID: "input"}}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.BaseURL = "bot", "owner", defaultBase
	freshWindow(b, "input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	for group := 0; group < 6; group++ {
		for index := 0; index < 3; index++ {
			snapshot.Items = append(snapshot.Items, api.Item{ID: "progress-" + string(rune('a'+group*3+index)), Kind: "assistant", TurnKey: "turn", Status: "completed", Text: "进展 " + string(rune('A'+group*3+index))})
		}
		b.output(t.Context(), p)
	}
	if len(*texts) != progressSendLimit || b.state.Window.ProgressUsed != progressSendLimit || len(b.state.Progress) != 3 {
		t.Fatalf("progress consumed reserved sends: texts=%#v window=%#v pending=%d", *texts, b.state.Window, len(b.state.Progress))
	}
	snapshot.Phase = "completed"
	snapshot.Items = append(snapshot.Items, api.Item{ID: "final", Kind: "assistant", TurnKey: "turn", Status: "completed", Text: "最终结论"})
	b.output(t.Context(), p)
	if len(*texts) != progressSendLimit+1 || b.state.Window.ProgressUsed != progressSendLimit || len(b.state.Progress) != 0 || !strings.Contains((*texts)[5], "进展 P") || !strings.HasSuffix((*texts)[5], "最终结论") {
		t.Fatalf("final did not merge reserved progress: texts=%#v window=%#v pending=%#v", *texts, b.state.Window, b.state.Progress)
	}
}

func TestWorkerStartUsesExactWeixinTurn(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	root := t.TempDir()
	snapshot := api.Snapshot{RuntimeOwner: "fixture-runtime", Connection: "ready", CurrentTurn: "wx-turn", Phase: "working", Items: []api.Item{
		{ID: "wx-user", Kind: "user", TurnKey: "wx-turn", RequestID: "wx-input"},
		{ID: "desktop-user", Kind: "user", TurnKey: "desktop-turn", RequestID: "desktop-input"},
		{ID: "task-start:wx", Kind: "assistant", TurnKey: "wx-turn", Status: "completed", Text: "手机发起的 Worker", Task: &api.TaskPresentation{ID: "wx", Title: "手机发起的 Worker", Status: "working"}},
		{ID: "task-start:desktop", Kind: "assistant", TurnKey: "desktop-turn", Status: "completed", Text: "桌面发起的 Worker", Task: &api.TaskPresentation{ID: "desktop", Title: "桌面发起的 Worker", Status: "working"}},
	}}
	b, err := Open(root, Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.BaseURL = "bot", "owner", defaultBase
	freshWindow(b, "wx-input")
	b.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	if len(*texts) != 1 || (*texts)[0] != "**手机发起的 Worker**" {
		t.Fatalf("worker start routed outside source turn: %#v", *texts)
	}
	if !b.ownsApproval(snapshot, api.Approval{Owner: "task", TaskID: "wx", TurnKey: "wx-worker-turn"}) || b.ownsApproval(snapshot, api.Approval{Owner: "task", TaskID: "desktop", TurnKey: "desktop-worker-turn"}) {
		t.Fatal("worker approval lost exact task source")
	}
	report := api.Snapshot{RuntimeOwner: "fixture-runtime", ReportSources: []api.ReportSource{{RuntimeOwner: "fixture-runtime", TurnKey: "report-turn", ReportID: "report-wx", Phase: "completed"}}, Connection: "ready", Phase: "completed", LastReceipt: api.Receipt{ID: "report-wx", Outcome: "accepted"}, Items: []api.Item{
		{ID: "report-final", Kind: "assistant", TurnKey: "report-turn", Status: "completed", Text: "Worker 已完成"},
	}}
	reopened, err := Open(root, Host{Snapshot: func() api.Snapshot { return report }, TaskForReport: func(id string) string {
		if id == "report-wx" {
			return "wx"
		}
		if id == "report-desktop" {
			return "desktop"
		}
		return ""
	}})
	if err != nil || !reopened.ownsApproval(api.Snapshot{RuntimeOwner: "fixture-runtime"}, api.Approval{Owner: "task", TaskID: "wx", TurnKey: "wx-worker-turn"}) {
		t.Fatal("worker approval source disappeared after restart", err)
	}
	reopened.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	if len(*texts) != 2 || (*texts)[1] != "Worker 已完成" {
		t.Fatal("Weixin Worker report was not delivered", *texts)
	}
	report.LastReceipt = api.Receipt{ID: "report-desktop", Outcome: "accepted"}
	report.ReportSources = []api.ReportSource{{RuntimeOwner: "fixture-runtime", TurnKey: "desktop-report-turn", ReportID: "report-desktop", Phase: "completed"}}
	report.Items = []api.Item{{ID: "desktop-report-final", Kind: "assistant", TurnKey: "desktop-report-turn", Status: "completed", Text: "桌面 Worker 已完成"}}
	reopened.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	if len(*texts) != 2 {
		t.Fatalf("desktop Worker report leaked to Weixin: %#v", *texts)
	}
}

func TestProgressOverflowFlushesHeadThenLargeMessage(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "working", Items: []api.Item{{ID: "user", Kind: "user", TurnKey: "turn", RequestID: "input"}}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	freshWindow(b, "input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	snapshot.Items = append(snapshot.Items, api.Item{ID: "seed", Kind: "assistant", TurnKey: "turn", Status: "completed", Text: "first"})
	b.output(t.Context(), p)
	snapshot.Items = append(snapshot.Items,
		api.Item{ID: "small", Kind: "assistant", TurnKey: "turn", Status: "completed", Text: strings.Repeat("s", 300)},
		api.Item{ID: "large", Kind: "assistant", TurnKey: "turn", Status: "completed", Text: strings.Repeat("L", 2200)},
	)
	b.output(t.Context(), p)
	b.output(t.Context(), p)
	if len(*texts) != 3 || (*texts)[0] != "first" || (*texts)[1] != strings.Repeat("s", 300) || (*texts)[2] != strings.Repeat("L", 2200) {
		t.Fatalf("overflow stalled a ready batch: lengths=%d pending=%#v", len(*texts), b.state.Progress)
	}
}

func TestQuestionDefersAtLastSlotAndFinalIncludesWindowHint(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	b, err := Open(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	freshWindow(b, "input")
	b.state.Window.Used = windowSendLimit - 1
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.mirrorItem(t.Context(), p, api.Item{ID: "question", Kind: "assistant", Text: "[Q1] 请选择\n[1] 甲\n[2] 乙", Status: "completed"}, "question")
	if len(*texts) != 0 || b.state.Outputs["item:question:0"].State != "" {
		t.Fatalf("question consumed the final reserve: %#v", *texts)
	}
	b.mirrorItem(t.Context(), p, api.Item{ID: "final", Kind: "assistant", Text: "最终回复", Status: "completed"}, "final")
	if len(*texts) != 1 || !strings.HasPrefix((*texts)[0], "最终回复") || !strings.Contains((*texts)[0], "发送 /r") || b.state.Window.Used != windowSendLimit {
		t.Fatalf("final reserve or hint failed: %#v, window=%#v", *texts, b.state.Window)
	}
}

func TestNativeQuestionCardAlsoKeepsFinalReserve(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	root := t.TempDir()
	control, err := textchannel.Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(root, Host{TextControl: control})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	freshWindow(b, "input")
	b.state.Window.Used = windowSendLimit - 1
	snapshot := api.Snapshot{Items: []api.Item{{ID: "user", Kind: "user", TurnKey: "turn", RequestID: "input"}}, Approvals: []api.Approval{{ID: "question", Owner: "conversation", TurnKey: "turn", Status: "pending", Choices: []api.Choice{{ID: "submit", Label: "提交"}}, Questions: []api.Question{{ID: "value", Title: "请输入", Type: "text", Required: true}}}}}
	b.mirrorControls(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"}, snapshot)
	if len(*texts) != 0 || b.state.Window.Used != windowSendLimit-1 {
		t.Fatalf("native question consumed terminal reserve: %#v", *texts)
	}
}

func TestRefreshReplaysOnlyUnattemptedMergedProgressAfterRestart(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	root := t.TempDir()
	submits := 0
	host := Host{Snapshot: func() api.Snapshot { return api.Snapshot{Connection: "ready"} }, Submit: func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		submits++
		return api.Receipt{}, nil
	}}
	b, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID, b.state.BaseURL = "bot", "owner", defaultBase
	freshWindow(b, "old")
	b.state.Window.Used = windowSendLimit
	for _, id := range []string{"one", "two", "three"} {
		b.queueProgress(api.Item{ID: id, Kind: "assistant", TurnKey: "turn", Text: id})
	}
	reopened, err := Open(root, host)
	if err != nil {
		t.Fatal(err)
	}
	freshWindow(reopened, "weixin:bot:refresh")
	reopened.state.Inbox = []inbound{{ID: "weixin:bot:refresh", Text: "/r", ContextToken: "ctx"}}
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	reopened.dispatch(t.Context(), p)
	if submits != 0 || len(*texts) != 1 || !strings.Contains((*texts)[0], "one\n\n——\n\ntwo\n\n——\n\nthree") || len(reopened.state.Progress) != 0 || reopened.state.Window.Used != 1 {
		t.Fatalf("refresh did not merge saved backlog: submits=%d texts=%#v state=%#v", submits, *texts, reopened.state.Progress)
	}
	reopened.dispatch(t.Context(), p)
	if len(*texts) != 1 {
		t.Fatal("duplicate refresh replayed the batch")
	}
}

func TestNewOrdinaryIngressFlushesPreviousPendingBatch(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", Phase: "working"}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	freshWindow(b, "old-input")
	b.state.Window.Used = windowSendLimit
	b.queueProgress(api.Item{ID: "older", Kind: "assistant", TurnKey: "old-turn", Text: "未发的旧进展"})
	freshWindow(b, "new-input")
	b.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"})
	if len(*texts) != 1 || (*texts)[0] != "未发的旧进展" || len(b.state.Progress) != 0 || b.state.Window.Used != 1 {
		t.Fatalf("fresh ingress did not restore pending work: %#v, state=%#v", *texts, b.state)
	}
}

func TestLastSlotApprovalHintKeepsCopyableCommandLast(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	b, err := Open(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	freshWindow(b, "input")
	b.state.Window.Used = windowSendLimit - 1
	b.sendTextOnce(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "fixture"}, "approval", "ctx", "**[A7] tool**\n[1] Allow\n\n**提示：发送 /approve A7 + 选项标号**")
	if len(*texts) != 1 || !strings.Contains((*texts)[0], "发送 /r") || !strings.HasSuffix((*texts)[0], "**提示：发送 /approve A7 + 选项标号**") {
		t.Fatalf("last-slot hint obscured approval command: %#v", *texts)
	}
}

func TestWeixinLocalCommandsDoNotSubmitModelInput(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	stops, submits := 0, 0
	host := Host{Snapshot: func() api.Snapshot { return api.Snapshot{Connection: "ready", Phase: "working"} }, Interrupt: func(context.Context) error { stops++; return nil }, Submit: func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		submits++
		return api.Receipt{}, nil
	}}
	b, err := Open(t.TempDir(), host)
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	for index, command := range []string{"/help", "/status", "/stop"} {
		id := "weixin:bot:" + string(rune('a'+index))
		freshWindow(b, id)
		b.state.Inbox = []inbound{{ID: id, Text: command, ContextToken: "ctx"}}
		b.dispatch(t.Context(), p)
	}
	if submits != 0 || stops != 1 || len(*texts) != 3 || !strings.Contains((*texts)[0], "/r") || !strings.Contains((*texts)[1], "本轮微信发送估计") || !strings.Contains((*texts)[2], "停止请求已提交") {
		t.Fatalf("local controls: submit=%d stop=%d texts=%#v", submits, stops, *texts)
	}
}

func TestFinalAlreadySentAsProgressDoesNotClaimOtherQueuedText(t *testing.T) {
	server, texts := sentTexts(t, nil)
	defer server.Close()
	snapshot := api.Snapshot{Connection: "ready", CurrentTurn: "turn", Phase: "working", Items: []api.Item{{ID: "user", Kind: "user", TurnKey: "turn", RequestID: "input"}, {ID: "done", Kind: "assistant", TurnKey: "turn", Status: "completed", Text: "done"}}}
	b, err := Open(t.TempDir(), Host{Snapshot: func() api.Snapshot { return snapshot }})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	freshWindow(b, "input")
	p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
	b.output(t.Context(), p)
	snapshot.Items = []api.Item{snapshot.Items[0], {ID: "older", Kind: "assistant", TurnKey: "turn", Status: "completed", Text: "older"}, snapshot.Items[1]}
	b.output(t.Context(), p)
	snapshot.Phase = "completed"
	b.output(t.Context(), p)
	if len(*texts) != 1 || (*texts)[0] != "done" || len(b.state.Progress) != 1 || b.state.Progress[0].ID != "older" || b.state.Outputs["item:older"].State != "" {
		t.Fatalf("queued text was falsely marked sent: %#v pending=%#v", *texts, b.state.Progress)
	}
}
