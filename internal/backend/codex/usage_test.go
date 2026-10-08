package codex

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestDreamRenewalAcceptsFirstUsageOfNewThread(t *testing.T) {
	s, f := sessionPair(t, "normal")
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		switch m.Method {
		case "thread/start":
			return map[string]any{"thread": nativeThread{ID: "next-thread"}, "model": "native-default"}, true
		case "turn/start":
			var p struct {
				ID string `json:"clientUserMessageId"`
			}
			json.Unmarshal(m.Params, &p)
			return map[string]any{"turn": nativeTurn{ID: p.ID + "-turn", Status: "inProgress"}}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	finish := func(thread, turn string, used, total int64) {
		t.Helper()
		f.emit(wireMessage{Method: "turn/started", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: turn, Status: "inProgress"}})})
		f.emit(wireMessage{Method: "thread/tokenUsage/updated", Params: raw(map[string]any{
			"threadId": thread, "turnId": turn,
			"tokenUsage": map[string]any{"last": map[string]int64{"totalTokens": used, "inputTokens": used - 1000, "outputTokens": 1000}, "total": map[string]int64{"totalTokens": total}, "modelContextWindow": 100000},
		})})
		f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: turn, Status: "completed"}})})
		awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	}
	if receipt, err := s.SubmitDream(t.Context(), api.Submission{ID: "dream-usage", Text: "system maintenance"}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	finish("thread-native", "dream-usage-turn", 80000, 9000000)
	if got := s.ConversationState().Usage; got.Used != 80000 {
		t.Fatal("old thread usage not established", got)
	}
	if err := s.RenewConversation(t.Context(), "dream-usage", "thread-native"); err != nil {
		t.Fatal(err)
	}
	if got := s.ConversationState().Usage; !got.ModelAt.IsZero() {
		t.Error("empty new thread retained old model activity", got)
	}
	if receipt, err := s.Submit(t.Context(), api.Submission{ID: "fresh-user", Text: "hello"}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	finish("next-thread", "fresh-user-turn", 40000, 40000)
	if got := s.ConversationState().Usage; got.Used != 40000 || got.Window != 100000 || got.ModelAt.IsZero() {
		t.Fatal("first usage of new thread was discarded", got)
	}
}

func TestUsageUsesLastContextAndLiveResidentEvidence(t *testing.T) {
	s := NewSession(SessionOptions{})
	s.binding.ThreadID = "resident"
	start := Notification{Method: "turn/started", Params: json.RawMessage(`{"threadId":"resident","turn":{"id":"turn","status":"inProgress"}}`)}
	usage := Notification{Method: "thread/tokenUsage/updated", ReceivedAt: time.Now().Round(0), Params: json.RawMessage(`{"threadId":"resident","turnId":"turn","tokenUsage":{"last":{"totalTokens":80000,"inputTokens":79000,"outputTokens":1000,"cachedInputTokens":12000,"cacheWriteInputTokens":500},"total":{"totalTokens":9000000},"modelContextWindow":100000}}`)}
	s.applyEvent(usage)
	if !s.usage.ModelAt.IsZero() {
		t.Fatal("history without live turn became warm")
	}
	s.applyEvent(start)
	invalid := usage
	invalid.Params = json.RawMessage(`{"threadId":"resident","turnId":"turn","tokenUsage":{"last":{"totalTokens":1,"inputTokens":1,"outputTokens":0},"total":{"totalTokens":1}}}`)
	s.applyEvent(invalid)
	if s.usageReason != "invalid_token_usage" || !s.usage.ModelAt.IsZero() {
		t.Fatal("invalid native gauge was not classified", s.usageReason, s.usage)
	}
	s.applyEvent(usage)
	if s.usage.Used != 80000 || s.usage.Window != 100000 || !s.usage.ModelAt.Equal(usage.ReceivedAt) || s.usage.InputTokens != 79000 || s.usage.OutputTokens != 1000 || s.usage.CacheReadTokens != 12000 || s.usage.CacheWriteTokens != 500 || s.usageReason != "live_token_usage" {
		t.Fatal(s.usage)
	}
	usage.ReceivedAt = usage.ReceivedAt.Add(time.Minute)
	s.applyEvent(usage)
	if s.usage.ModelAt.Equal(usage.ReceivedAt) {
		t.Fatal("duplicate refreshed cache age")
	}
	for _, payload := range []string{
		`{"threadId":"worker","turnId":"turn","tokenUsage":{"total":{"totalTokens":99999999}}}`,
		`{"threadId":"resident","turnId":"old","tokenUsage":{"total":{"totalTokens":99999999}}}`,
	} {
		s.applyEvent(Notification{Method: usage.Method, Params: json.RawMessage(payload), ReceivedAt: time.Now()})
	}
	if s.usage.Used != 80000 {
		t.Fatal("foreign usage affected context")
	}
	s.applyEvent(Notification{Method: "item/completed", Params: json.RawMessage(`{"threadId":"resident","turnId":"turn","item":{"id":"compact","type":"contextCompaction"}}`)})
	if !s.usage.ModelAt.IsZero() || s.usageReason != "native_compaction" {
		t.Fatal("compaction retained gauge")
	}
	usage.EmittedAtMS = usage.ReceivedAt.Add(-20 * time.Minute).UnixMilli()
	s.applyEvent(usage)
	if s.usage.ModelAt.UnixMilli() != usage.EmittedAtMS {
		t.Fatal("queued native event gained freshness")
	}
	s.resetProjection()
	s.applyEvent(usage)
	if !s.usage.ModelAt.IsZero() || s.usageReason != "restore_requires_live_usage" {
		t.Fatal("restore established cache warmth")
	}
}
