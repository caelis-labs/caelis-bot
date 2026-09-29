package codex

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUsageUsesLastContextAndLiveResidentEvidence(t *testing.T) {
	s := NewSession(SessionOptions{})
	s.binding.ThreadID = "resident"
	start := Notification{Method: "turn/started", Params: json.RawMessage(`{"threadId":"resident","turn":{"id":"turn","status":"inProgress"}}`)}
	usage := Notification{Method: "thread/tokenUsage/updated", ReceivedAt: time.Now().Round(0), Params: json.RawMessage(`{"threadId":"resident","turnId":"turn","tokenUsage":{"last":{"totalTokens":80000,"inputTokens":79000,"outputTokens":1000},"total":{"totalTokens":9000000},"modelContextWindow":100000}}`)}
	s.applyEvent(usage)
	if !s.usage.ModelAt.IsZero() {
		t.Fatal("history without live turn became warm")
	}
	s.applyEvent(start)
	s.applyEvent(usage)
	if s.usage.Used != 80000 || s.usage.Window != 100000 || !s.usage.ModelAt.Equal(usage.ReceivedAt) {
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
	if !s.usage.ModelAt.IsZero() {
		t.Fatal("compaction retained gauge")
	}
	usage.EmittedAtMS = usage.ReceivedAt.Add(-20 * time.Minute).UnixMilli()
	s.applyEvent(usage)
	if s.usage.ModelAt.UnixMilli() != usage.EmittedAtMS {
		t.Fatal("queued native event gained freshness")
	}
	s.resetProjection()
	s.applyEvent(usage)
	if !s.usage.ModelAt.IsZero() {
		t.Fatal("restore established cache warmth")
	}
}
