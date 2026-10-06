package codex

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestReconnectReadsOnlyLatestHumanSummary(t *testing.T) {
	s, f := sessionPair(t, "")
	f.mu.Lock()
	f.history = []nativeTurn{{ID: "old", Status: "completed", Items: []nativeItem{{ID: "tool", Type: "mcpToolCall", Result: raw(map[string]string{"image": "native-only"})}}}, {ID: "latest", Status: "completed", Items: []nativeItem{{ID: "user", Type: "userMessage", Content: []nativeInput{{Type: "text", Text: "recent topic"}}}, {ID: "reply", Type: "agentMessage", Text: "recent answer"}}}}
	peer := f.peer
	f.mu.Unlock()
	peer.Close()
	awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "offline" })
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	v := s.Snapshot()
	if len(v.Items) != 2 || v.Items[0].Text != "recent topic" || v.HasEarlier || !v.CanSend {
		t.Fatal(v)
	}
	f.mu.Lock()
	calls := len(f.pageCalls)
	excluded := f.resumeExcluded
	f.mu.Unlock()
	if !excluded {
		t.Fatal("resume hydrated history")
	}
	if err := s.LoadEarlier(testContext(t)); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pageCalls) != calls {
		t.Fatal("older IM requested native history")
	}
}

func TestOptionalRecentSyncFailureDoesNotBlockConnection(t *testing.T) {
	s, f := sessionPair(t, "")
	f.mu.Lock()
	f.failPage = true
	peer := f.peer
	f.mu.Unlock()
	peer.Close()
	awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "offline" })
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if v := s.Snapshot(); v.Connection != "ready" || !v.CanSend {
		t.Fatal("display error blocked control", v)
	}
	s.mu.Lock()
	retry := s.residentSyncNeeded
	s.mu.Unlock()
	if !retry {
		t.Fatal("optional sync not scheduled for retry")
	}
}

func TestMessageWebLinkSurvivesHistoryProjection(t *testing.T) {
	s := &Session{opts: SessionOptions{Directory: t.TempDir()}}
	s.resetProjection()
	text := "[网站](https://example.com/help) and [结果](result.txt)"
	s.applyItem("turn", nativeItem{ID: "reply", Type: "agentMessage", Text: text}, true)
	got := s.state.Items[0]
	if got.Text != "[网站](https://example.com/help) and 结果" || len(got.Artifacts) != 1 || got.Artifacts[0].Name != "result.txt" {
		t.Fatal("web link became a local result", got)
	}
}

func TestMissingNativeThreadCanOnlyReplaceNeverSubmittedBinding(t *testing.T) {
	for _, state := range []string{"empty", "legacy", "submitted", "unknown", "other-error"} {
		t.Run(state, func(t *testing.T) {
			s, f := sessionPair(t, "early-terminal")
			if state == "submitted" {
				sendSynthetic(t, s, "already-submitted")
			}
			s.mu.Lock()
			if state == "legacy" {
				s.binding.Unsubmitted = false
			}
			if state == "unknown" {
				s.binding.Pending = &pendingSubmission{ID: "unknown-send"}
			}
			if err := s.save(); err != nil {
				t.Fatal(err)
			}
			s.mu.Unlock()
			starts := 0
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				switch m.Method {
				case "thread/turns/list":
					return &NativeError{Code: -32600, Message: "thread not loaded: thread-native"}, true
				case "thread/resume":
					message := "no rollout found for thread id thread-native"
					if state == "other-error" {
						message = "unrelated failure"
					}
					return &NativeError{Code: -32600, Message: message}, true
				case "thread/start":
					starts++
					return map[string]any{"thread": nativeThread{ID: "replacement"}}, true
				}
				return nil, false
			}
			peer := f.peer
			f.mu.Unlock()
			peer.Close()
			awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "offline" })
			err := s.Connect(testContext(t))
			if state == "empty" {
				if err != nil || starts != 1 || !s.Snapshot().CanSend {
					t.Fatal("empty connection not recovered", err)
				}
			} else if err == nil || starts != 0 {
				t.Fatal("replaced a binding without proof", state, err)
			}
		})
	}
}
