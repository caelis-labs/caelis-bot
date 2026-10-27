package weixin

import (
	"context"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

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
	if len(*texts) != 1 || !strings.Contains((*texts)[0], "请回复 Bot") || !strings.HasSuffix((*texts)[0], "**提示：发送 /approve A7 + 选项标号**") {
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
