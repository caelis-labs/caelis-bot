package codex

import (
	"fmt"
	"slices"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestPagedHistoryRestoresRecentThenPrependsWithoutReplayingState(t *testing.T) {
	s, f := sessionPair(t, "")
	all := make([]nativeTurn, 45)
	for i := range all {
		id := fmt.Sprintf("turn-%02d", i)
		all[i] = nativeTurn{ID: id, Status: "completed", Items: []nativeItem{
			{ID: "user", Type: "userMessage", Content: []nativeInput{{Type: "text", Text: id}}},
			{ID: "answer", Type: "agentMessage", Text: "reply " + id},
		}}
	}
	// An older stored lifecycle must never change today's state when hydrated.
	all[0].Status = "inProgress"
	all[0].Items = append(all[0].Items, nativeItem{ID: "worker", Type: "collabAgentToolCall", ReceiverThreadIDs: []string{"historic-worker"}})
	page := func(lo, hi int, next string) turnPage {
		data := slices.Clone(all[lo:hi])
		slices.Reverse(data)
		return turnPage{Data: data, NextCursor: next}
	}
	f.mu.Lock()
	f.pages = map[string]turnPage{"": page(25, 45, "older-25"), "older-25": page(5, 25, "older-5"), "older-5": page(0, 5, "")}
	peer := f.peer
	f.mu.Unlock()
	peer.Close()
	awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "offline" })
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	v := s.Snapshot()
	if len(v.Items) != 40 || v.Items[0].Text != "turn-25" || !v.HasEarlier || v.Phase != "completed" {
		t.Fatalf("bad recent window: %d %+v", len(v.Items), v)
	}
	f.mu.Lock()
	excluded := f.resumeExcluded
	f.failPage = true
	f.mu.Unlock()
	if !excluded {
		t.Fatal("resume hydrated full history")
	}
	if s.LoadEarlier(testContext(t)) == nil || len(s.Snapshot().Items) != 40 {
		t.Fatal("failed page erased current messages")
	}
	f.mu.Lock()
	f.failPage = false
	f.mu.Unlock()
	for range 2 {
		if err := s.LoadEarlier(testContext(t)); err != nil {
			t.Fatal(err)
		}
	}
	v = s.Snapshot()
	if len(v.Items) != 90 || v.HasEarlier || v.Phase != "completed" || !v.CanSend {
		t.Fatalf("bad hydrated state: count=%d phase=%s", len(v.Items), v.Phase)
	}
	for i := range all {
		if v.Items[i*2].Text != all[i].ID {
			t.Fatal("history order or identity changed")
		}
	}
	s.mu.Lock()
	children := len(s.children)
	run := s.run
	s.mu.Unlock()
	if children != 0 || run != "" {
		t.Fatal("history hydration replayed execution")
	}
	if len(s.RecentSnapshot().Items) != 2 {
		t.Fatal("pet received old history")
	}
}

func TestEarlierOverlapPreservesNewerLiveProjection(t *testing.T) {
	s, f := sessionPair(t, "")
	s.mu.Lock()
	s.historyPaged = true
	s.historyCursor = "older"
	s.state.HasEarlier = true
	s.applyTurn(nativeTurn{ID: "current", Status: "inProgress", Items: []nativeItem{{ID: "reply", Type: "agentMessage", Text: "new streamed content"}}}, false)
	s.update()
	s.mu.Unlock()
	f.mu.Lock()
	f.pages = map[string]turnPage{"older": {Data: []nativeTurn{
		{ID: "current", Status: "completed", Items: []nativeItem{{ID: "reply", Type: "agentMessage", Text: "stale stored content"}}},
		{ID: "old", Status: "completed", Items: []nativeItem{{ID: "reply", Type: "agentMessage", Text: "older"}}},
	}}}
	f.mu.Unlock()
	if err := s.LoadEarlier(testContext(t)); err != nil {
		t.Fatal(err)
	}
	v := s.Snapshot()
	if len(v.Items) != 2 || v.Items[1].Text != "new streamed content" || !v.CanSteer || !v.CanInterrupt {
		t.Fatal("old read overwrote live work")
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
