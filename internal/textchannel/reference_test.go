package textchannel

import (
	"context"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestConfirmedCardReferenceUsesExactNativeChoiceAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	var sent []api.Decision
	decide := func(_ context.Context, d api.Decision) error { sent = append(sent, d); return nil }
	s, _ := Open(root, decide)
	a := api.Approval{ID: "native", TurnKey: "worker-turn", Owner: "task", Title: "创建 GitHub Issue", Target: "repo/example", Status: "pending", Choices: []api.Choice{{ID: "deny", Label: "拒绝"}, {ID: "once", Label: "仅允许本次", Scope: "once"}}}
	card, _, err := s.Card(a)
	if err != nil || !strings.Contains(card, "[A1] 创建 GitHub Issue\n目标：repo/example") || !strings.Contains(card, "[2] 仅允许本次") || !strings.HasSuffix(card, "执行审批请输入\n/approve A1 1") {
		t.Fatalf("card %q: %v", card, err)
	}
	if err := s.BindCard("telegram", "chat", "77", a); err != nil {
		t.Fatal(err)
	}
	s, _ = Open(root, decide)
	snap := api.Snapshot{Approvals: []api.Approval{a}}
	in := Inbound{Channel: "telegram", Conversation: "chat", ID: "m1", Text: "2", Reference: &Reference{Conversation: "chat", MessageID: "77"}}
	if feedback, handled := s.HandleReference(t.Context(), in, snap); !handled || !strings.Contains(feedback, "已提交") {
		t.Fatalf("reply: %q %v", feedback, handled)
	}
	if len(sent) != 1 || sent[0].ID != "native" || sent[0].Choice != "once" {
		t.Fatal(sent)
	}
	in.ID = "m2"
	if feedback, handled := s.HandleReference(t.Context(), in, snap); !handled || !strings.Contains(feedback, "已提交") || len(sent) != 1 {
		t.Fatalf("stale: %q %v %#v", feedback, handled, sent)
	}
	in.Reference.MessageID = "unbound"
	if _, handled := s.HandleReference(t.Context(), in, snap); handled {
		t.Fatal("unbound quote gained authority")
	}
	in.Reference.MessageID = "77"
	in.Reference.Conversation = "other"
	if _, handled := s.HandleReference(t.Context(), in, snap); handled {
		t.Fatal("cross-conversation quote gained authority")
	}
}

func TestMultiQuestionQuotedNumberIsAmbiguousAndSecretReplyIsMarked(t *testing.T) {
	var sent []api.Decision
	s, _ := Open(t.TempDir(), func(_ context.Context, d api.Decision) error { sent = append(sent, d); return nil })
	a := api.Approval{ID: "form", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "answer", Label: "提交"}}, Questions: []api.Question{{ID: "one", Title: "一", Type: "text", Required: true}, {ID: "two", Title: "二", Type: "text", Required: true}}}
	_, _, _ = s.Card(a)
	if err := s.BindCard("telegram", "chat", "card", a); err != nil {
		t.Fatal(err)
	}
	in := Inbound{Channel: "telegram", Conversation: "chat", ID: "reply", Text: "1", Reference: &Reference{Conversation: "chat", MessageID: "card"}}
	if feedback, handled := s.HandleReference(t.Context(), in, api.Snapshot{Approvals: []api.Approval{a}}); !handled || !strings.Contains(feedback, "歧义") || len(sent) != 0 {
		t.Fatalf("ambiguous: %q %v %#v", feedback, handled, sent)
	}
	a.Status = "resolved"
	in.ID = "stale"
	if feedback, handled := s.HandleReference(t.Context(), in, api.Snapshot{Approvals: []api.Approval{a}}); !handled || !strings.Contains(feedback, "已处理") {
		t.Fatalf("resolved ambiguous card: %q %v", feedback, handled)
	}
	secret := api.Approval{ID: "secret", Owner: "task", Status: "pending", Questions: []api.Question{{ID: "pin", Type: "text", Required: true, Secret: true}}}
	_, _, _ = s.Card(secret)
	if err := s.BindCard("weixin", "owner", "998", secret); err != nil {
		t.Fatal(err)
	}
	secretIn := Inbound{Channel: "weixin", Conversation: "ctx", ID: "reply2", Text: "PRIVATE", Reference: &Reference{Conversation: "owner", MessageID: "998"}}
	if s.SecretPrompt(secretIn) == "" {
		t.Fatal("marked secret reply not classified before persistence")
	}
	if feedback, handled := s.HandleReference(t.Context(), secretIn, api.Snapshot{Approvals: []api.Approval{secret}}); !handled || !strings.Contains(feedback, "已提交") || len(sent) != 1 || sent[0].Answers["pin"][0] != "PRIVATE" {
		t.Fatalf("secret: %q %v %#v", feedback, handled, sent)
	}
}

func TestChangedApprovalTargetInvalidatesOldTextAndReference(t *testing.T) {
	s, _ := Open(t.TempDir(), func(context.Context, api.Decision) error { t.Fatal("stale decision dispatched"); return nil })
	a := api.Approval{ID: "native", Owner: "task", Status: "pending", Target: "repo/old", Choices: []api.Choice{{ID: "allow", Label: "允许"}}}
	_, id, _ := s.Card(a)
	if err := s.BindCard("telegram", "chat", "card", a); err != nil {
		t.Fatal(err)
	}
	a.Target = "repo/new"
	snap := api.Snapshot{Approvals: []api.Approval{a}}
	if feedback := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "ctx", ID: "slash", Text: "/approve " + id + " 1"}, snap); !strings.Contains(feedback, "已变化") {
		t.Fatal(feedback)
	}
	if feedback, handled := s.HandleReference(t.Context(), Inbound{Channel: "telegram", Conversation: "chat", ID: "quote", Text: "1", Reference: &Reference{Conversation: "chat", MessageID: "card"}}, snap); !handled || !strings.Contains(feedback, "已变化") {
		t.Fatalf("quote: %q %v", feedback, handled)
	}
}
