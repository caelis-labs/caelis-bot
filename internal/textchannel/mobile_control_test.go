package textchannel

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestControlNoticeKeepsFirstFeedbackTimeAcrossDuplicateAndRestart(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var pushed []Notice
	s.SetNoticeObserver(func(n Notice) { pushed = append(pushed, n) })
	in := Inbound{Channel: "telegram", Conversation: "owner", ID: "same"}
	if got := s.Publish(in, "已提交"); got != "已提交" {
		t.Fatal(got)
	}
	before := s.Notices()
	if len(before) != 1 || before[0].SeenAt <= 0 {
		t.Fatal(before)
	}
	if len(pushed) != 1 || pushed[0] != before[0] {
		t.Fatal("notice was not appended once to local chat", pushed)
	}
	if got := s.Publish(in, "different"); got != "已提交" {
		t.Fatal("duplicate feedback changed", got)
	}
	if len(pushed) != 1 {
		t.Fatal("duplicate notice was appended twice", pushed)
	}
	reopened, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	after := reopened.Notices()
	if len(after) != 1 || after[0].SeenAt != before[0].SeenAt || after[0].Text != "已提交" {
		t.Fatal("receipt presentation changed after restart", after)
	}
}

func TestMultiQuestionCollectEditRestartAndSubmitOnce(t *testing.T) {
	root := t.TempDir()
	var decisions []api.Decision
	decide := func(_ context.Context, d api.Decision) error { decisions = append(decisions, d); return nil }
	s, err := Open(root, decide)
	if err != nil {
		t.Fatal(err)
	}
	a := api.Approval{ID: "original", TurnKey: "child", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "answer", Label: "提交回答"}}, Questions: []api.Question{{ID: "first", Title: "第一项", Type: "text", Required: true}, {ID: "second", Title: "第二项", Type: "select", Required: true, Options: []api.Choice{{ID: "a", Label: "方案甲"}, {ID: "b", Label: "方案乙"}}}}}
	card, _, err := s.Card(a)
	if err != nil || !strings.Contains(card, "[Q1] 第一项") || !strings.Contains(card, "[Q2] 第二项") || !strings.Contains(card, "/answer Q2 1") {
		t.Fatalf("card: %q %v", card, err)
	}
	snap := api.Snapshot{Approvals: []api.Approval{a}}
	first := Inbound{Channel: "weixin", Conversation: "owner", ID: "one", Text: "/answer Q1 初稿"}
	if got := s.Handle(t.Context(), first, snap); !strings.Contains(got, "仍需回答 Q2") || len(decisions) != 0 {
		t.Fatalf("first: %q %#v", got, decisions)
	}
	first.ID, first.Text = "edit", "/answer Q1 修订稿"
	if got := s.Handle(t.Context(), first, snap); !strings.Contains(got, "仍需回答 Q2") || len(decisions) != 0 {
		t.Fatalf("edit: %q %#v", got, decisions)
	}
	reopened, err := Open(root, decide)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Handle(t.Context(), Inbound{Channel: "telegram", Conversation: "chat", ID: "two", Text: "/answer Q2 2"}, snap); !strings.Contains(got, "已提交") {
		t.Fatal(got)
	}
	if len(decisions) != 1 || decisions[0].ID != "original" || decisions[0].Choice != "answer" || decisions[0].Answers["first"][0] != "修订稿" || decisions[0].Answers["second"][0] != "b" {
		t.Fatalf("native decision: %#v", decisions)
	}
	if got := reopened.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "owner", ID: "stale", Text: "/answer Q1 再改"}, snap); !strings.Contains(got, "已提交") || len(decisions) != 1 {
		t.Fatalf("repeat: %q %#v", got, decisions)
	}
}

func TestMultipleNumericAndBooleanFieldAnswers(t *testing.T) {
	var decisions []api.Decision
	s, _ := Open(t.TempDir(), func(_ context.Context, d api.Decision) error { decisions = append(decisions, d); return nil })
	a := api.Approval{ID: "form", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "accept", Label: "提交"}, {ID: "decline", Label: "拒绝"}}, Questions: []api.Question{
		{ID: "tags", Title: "标签", Type: "select", Multiple: true, Required: true, Options: []api.Choice{{ID: "red", Label: "红"}, {ID: "green", Label: "绿"}, {ID: "blue", Label: "蓝"}}},
		{ID: "count", Title: "数量", Type: "integer", Required: true},
		{ID: "ratio", Title: "比例", Type: "number", Required: true},
		{ID: "enabled", Title: "启用", Type: "boolean", Required: true},
	}}
	card, _, err := s.Card(a)
	if err != nil || !strings.Contains(card, "/answer Q1 1,2") || !strings.Contains(card, "[1] 是\n[2] 否\n\n回答请输入\n/answer Q4 1") {
		t.Fatalf("card: %q %v", card, err)
	}
	snap := api.Snapshot{Approvals: []api.Approval{a}}
	for id, command := range []string{"/answer Q1 1,1", "/answer Q2 2.5", "/answer Q1 1,3", "/answer Q2 42", "/answer Q3 3.5", "/answer Q4 1"} {
		got := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "owner", ID: string(rune('a' + id)), Text: command}, snap)
		if id < 2 && !strings.Contains(got, "无效") {
			t.Fatalf("invalid %q: %q", command, got)
		}
	}
	if len(decisions) != 1 || decisions[0].Choice != "accept" || strings.Join(decisions[0].Answers["tags"], ",") != "red,blue" || decisions[0].Answers["count"][0] != "42" || decisions[0].Answers["ratio"][0] != "3.5" || decisions[0].Answers["enabled"][0] != "true" {
		t.Fatalf("form decision: %#v", decisions)
	}
}

func TestURLApprovalListsNativeDecisionsAndClassifiesLocalAddress(t *testing.T) {
	for _, tc := range []struct {
		url   string
		local bool
	}{{"https://accounts.example.test/authorize", false}, {"http://127.0.0.1:49152/callback", true}, {"http://192.168.1.9/login", true}} {
		t.Run(tc.url, func(t *testing.T) {
			var decisions []api.Decision
			s, _ := Open(t.TempDir(), func(_ context.Context, d api.Decision) error { decisions = append(decisions, d); return nil })
			a := api.Approval{ID: "url-native", Owner: "conversation", Status: "pending", URL: tc.url, Choices: []api.Choice{{ID: "accept", Label: "已完成授权"}, {ID: "decline", Label: "拒绝"}, {ID: "cancel", Label: "取消"}}}
			card, id, err := s.Card(a)
			if err != nil || !strings.Contains(card, tc.url) || !strings.Contains(card, "打开链接本身不代表成功") || strings.Contains(card, "本机或私网") != tc.local || len(cardCommands(card, "/approve ")) != 1 || !strings.Contains(card, "[3] 取消") {
				t.Fatalf("card %q, id %q, err %v", card, id, err)
			}
			if len(decisions) != 0 {
				t.Fatal("link display submitted a decision")
			}
			got := s.Handle(t.Context(), Inbound{Channel: "telegram", Conversation: "chat", ID: "confirm", Text: "/approve " + id + " 1"}, api.Snapshot{Approvals: []api.Approval{a}})
			if !strings.Contains(got, "等待 Runtime 确认") || len(decisions) != 1 || decisions[0].ID != "url-native" || decisions[0].Choice != "accept" {
				t.Fatalf("decision %q %#v", got, decisions)
			}
		})
	}
}

func TestMarkedSecretAnswerNeverEntersPersistentControlState(t *testing.T) {
	root := t.TempDir()
	const secret = "PRIVATE_SENTINEL_VALUE"
	var decisions []api.Decision
	s, _ := Open(root, func(_ context.Context, d api.Decision) error { decisions = append(decisions, d); return nil })
	a := api.Approval{ID: "native", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "answer", Label: "提交回答"}}, Questions: []api.Question{{ID: "credential", Title: "访问码", Type: "text", Required: true, Secret: true}, {ID: "reason", Title: "用途", Type: "text", Required: true}}}
	card, _, err := s.Card(a)
	if err != nil || !strings.Contains(card, "敏感字段") {
		t.Fatal(card, err)
	}
	command := "/answer Q1 " + secret
	if !s.IsSecretCommand(command) {
		t.Fatal("marked secret command was not identified")
	}
	snap := api.Snapshot{Approvals: []api.Approval{a}}
	if got := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "ctx", ID: "one", Text: command}, snap); !strings.Contains(got, "仍需回答 Q2") {
		t.Fatal(got)
	}
	for _, value := range []string{card, strings.Join(noticeTexts(s.Notices()), "\n")} {
		if strings.Contains(value, secret) {
			t.Fatal("secret escaped into mirrored text")
		}
	}
	b, err := os.ReadFile(s.path)
	if err != nil || strings.Contains(string(b), secret) {
		t.Fatal("secret escaped into persistent control state", err)
	}
	reopened, _ := Open(root, s.decide)
	if got := reopened.Handle(t.Context(), Inbound{Channel: "telegram", Conversation: "chat", ID: "two", Text: "/answer Q2 audit"}, snap); !strings.Contains(got, "仍需回答 Q1") || len(decisions) != 0 {
		t.Fatalf("restart lost safety: %q %#v", got, decisions)
	}
	if got := reopened.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "ctx", ID: "three", Text: command}, snap); !strings.Contains(got, "已提交") || len(decisions) != 1 || decisions[0].Answers["credential"][0] != secret {
		t.Fatalf("direct native answer: %q %#v", got, decisions)
	}
	b, err = os.ReadFile(s.path)
	if err != nil || strings.Contains(string(b), secret) {
		t.Fatal("secret persisted after native decision", err)
	}
}

func TestPreDispatchRuntimeValidationKeepsOriginalRequestEditable(t *testing.T) {
	var attempts []api.Decision
	s, _ := Open(t.TempDir(), func(_ context.Context, d api.Decision) error {
		attempts = append(attempts, d)
		if d.Answers["amount"][0] == "0" {
			return api.DecisionValidationError{Message: "数字不符合要求"}
		}
		return nil
	})
	a := api.Approval{ID: "form", Status: "pending", Choices: []api.Choice{{ID: "accept", Label: "提交"}}, Questions: []api.Question{{ID: "amount", Type: "integer", Required: true}}}
	_, id, _ := s.Card(a)
	snap := api.Snapshot{Approvals: []api.Approval{a}}
	if got := s.Handle(t.Context(), Inbound{Channel: "weixin", Conversation: "ctx", ID: "one", Text: "/answer " + id + " 0"}, snap); !strings.Contains(got, "可修改回答后重试") {
		t.Fatal(got)
	}
	if _, claimed := s.Claimed(a.ID); claimed {
		t.Fatal("pre-dispatch validation claimed native request")
	}
	if got := s.Handle(t.Context(), Inbound{Channel: "telegram", Conversation: "chat", ID: "two", Text: "/answer " + id + " 4"}, snap); !strings.Contains(got, "已提交") || len(attempts) != 2 || attempts[1].Answers["amount"][0] != "4" {
		t.Fatalf("correction: %q %#v", got, attempts)
	}
}

func TestSecretIngressRecoveryUsesOriginalClaimWithoutReplay(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	a := api.Approval{ID: "native", Status: "pending", Choices: []api.Choice{{ID: "answer", Label: "提交"}}, Questions: []api.Question{{ID: "password", Type: "text", Secret: true, Required: true}}}
	_, id, _ := s.Card(a)
	in := Inbound{Channel: "weixin", Conversation: "ctx", ID: "secret-message"}
	if got := s.RecoverSecretInput(in, id); !strings.Contains(got, "重新发送") {
		t.Fatal(got)
	}
	s.mu.Lock()
	s.state.Claims[a.ID] = claim{State: "unknown", Feedback: "决定结果待核对；不会重复提交。"}
	_ = s.save()
	s.mu.Unlock()
	other := Inbound{Channel: "weixin", Conversation: "ctx", ID: "later-secret-message"}
	if got := s.RecoverSecretInput(other, id); !strings.Contains(got, "待核对") {
		t.Fatal(got)
	}
	if got := s.RecoverSecretInput(other, id); !strings.Contains(got, "待核对") {
		t.Fatal("repeat changed result:", got)
	}
}

func noticeTexts(notices []Notice) []string {
	result := make([]string, 0, len(notices))
	for _, n := range notices {
		result = append(result, n.Text)
	}
	return result
}
