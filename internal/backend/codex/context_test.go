package codex

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestContextAcceptanceAndLazyRenewalRetainChat(t *testing.T) {
	s, f := sessionPair(t, "normal")
	var consumed atomic.Bool
	s.opts.BotTools = &api.ToolConnection{RuntimeVersion: "new-version", PrepareContext: func(context.Context) (api.ContextSeed, error) {
		return api.ContextSeed{Text: "[private handoff] old handoff\n", HandoffDigest: "original-digest"}, nil
	}, ConsumeContext: func(api.ContextSeed) error { consumed.Store(true); return nil }}
	var starts atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		switch m.Method {
		case "thread/start":
			if strings.Contains(string(m.Params), "thread-native") || strings.Contains(string(m.Params), "tool_search_output") {
				t.Error("fresh thread request copied old thread or search history")
			}
			starts.Add(1)
			return map[string]any{"thread": nativeThread{ID: "next-thread"}, "model": "native-default"}, true
		case "turn/start":
			var p struct {
				Thread string        `json:"threadId"`
				ID     string        `json:"clientUserMessageId"`
				Input  []nativeInput `json:"input"`
			}
			json.Unmarshal(m.Params, &p)
			if p.ID == "first-user" {
				if len(p.Input) != 1 || !strings.Contains(p.Input[0].Text, "old handoff") || !strings.HasSuffix(p.Input[0].Text, "hello") {
					t.Error("context missing")
				}
				if consumed.Load() {
					t.Error("consumed before native acceptance")
				}
			} else if p.ID == "second-user" && (len(p.Input) != 1 || p.Input[0].Text != "hello") {
				t.Error("same session reinjected memory")
			}
			return map[string]any{"turn": nativeTurn{ID: p.ID + "-turn", Status: "completed", Items: []nativeItem{{ID: "input", Type: "userMessage", ClientID: p.ID, Content: p.Input}, {ID: "reply", Type: "agentMessage", Phase: "final_answer", Text: "Completed; no pending work."}}}}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	for _, id := range []string{"first-user", "second-user"} {
		r, err := s.Submit(t.Context(), api.Submission{ID: id, Text: "hello"}, nil)
		if err != nil || r.Outcome != "accepted" {
			t.Fatal(r, err)
		}
	}
	if !consumed.Load() {
		t.Fatal("handoff not consumed")
	}
	for _, i := range s.Snapshot().Items {
		if i.Kind == "user" && i.Text != "hello" {
			t.Fatal("private context leaked to chat", i.Text)
		}
	}
	r, err := s.SubmitDream(t.Context(), api.Submission{ID: "dream-test", Text: "system maintenance"})
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	if starts.Load() != 0 {
		t.Fatal("Dream eagerly created thread")
	}
	view := s.Snapshot()
	if !view.Quiet || len(view.Items) != 5 || view.Items[4].Text != "Completed; no pending work." {
		t.Fatal("recap projection", view)
	}
	beforeGeneration := s.BotPluginGeneration()
	if err := s.RenewConversation(t.Context(), "dream-test", "thread-native"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewConversation(t.Context(), "dream-test", "thread-native"); err != nil || starts.Load() != 1 {
		t.Fatal("duplicate rotation", err)
	}
	if s.BotPluginGeneration() == beforeGeneration {
		t.Fatal("fresh thread retained the old MCP directory generation")
	}
	if len(s.Snapshot().Items) != 5 || s.binding.Context.Injected {
		t.Fatal("chat lost or new context skipped")
	}
	restored := NewSession(s.opts)
	if restored.ConversationState().RuntimeVersion != "new-version" || restored.ConversationState().DesiredRuntimeVersion != "new-version" || restored.binding.ThreadID != "next-thread" || len(restored.binding.PastThreads) != 1 {
		t.Fatal("binding not durable")
	}
}

func TestConversationObservedOnlyAfterNativeRestore(t *testing.T) {
	s, f := sessionPair(t, "normal")
	if err := s.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
	restored := NewSession(s.opts)
	restored.start = s.start
	t.Cleanup(func() { _ = restored.Close(testContext(t)) })
	if got := restored.ConversationState(); got.Session != "thread-native" || got.Turn != "" || got.Observed {
		t.Fatal("persisted binding mistaken for restored history", got)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method != "thread/resume" {
			return nil, false
		}
		close(entered)
		<-release
		return map[string]any{"thread": nativeThread{ID: "thread-native", Turns: []nativeTurn{{ID: "dream-turn", Status: "completed"}}}, "model": "native-default"}, true
	}
	f.mu.Unlock()
	connected := make(chan error, 1)
	go func() { connected <- restored.Connect(testContext(t)) }()
	select {
	case <-entered:
	case <-testContext(t).Done():
		t.Fatal("resume did not start")
	}
	if got := restored.ConversationState(); got.Observed || got.Turn != "" {
		t.Fatal("incomplete native resume exposed as observed", got)
	}
	once.Do(func() { close(release) })
	if err := <-connected; err != nil {
		t.Fatal(err)
	}
	if got := restored.ConversationState(); !got.Observed || got.Turn != "dream-turn" || got.Status != "completed" {
		t.Fatal("native history not observed", got)
	}
	restored.mu.Lock()
	restored.applyTurn(nativeTurn{ID: "background-report", Status: "inProgress"}, false)
	restored.update()
	restored.mu.Unlock()
	if got := restored.ConversationState(); !got.Observed || got.Idle {
		t.Fatal("observed activity confused with idle", got)
	}
	restored.mu.Lock()
	restored.run = "" // No producer for the synthetic background turn.
	restored.mu.Unlock()
}

func TestDreamCancelTargetsOnlyMaintenanceTurn(t *testing.T) {
	s, f := sessionPair(t, "normal")
	s.mu.Lock()
	s.binding.Dreams = map[string]dreamRecord{"dream-cancel": {Thread: s.binding.ThreadID}}
	s.binding.Scheduled["dream-cancel"] = "maintenance"
	s.lastTurn, s.run = "maintenance", "maintenance"
	s.runs["maintenance"] = "inProgress"
	s.childRuns["worker"] = "worker-run"
	s.mu.Unlock()
	var interrupted atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method != "turn/interrupt" {
			return nil, false
		}
		var p map[string]string
		json.Unmarshal(m.Params, &p)
		if p["threadId"] != "thread-native" || p["turnId"] != "maintenance" {
			t.Error("interrupted other work")
		}
		interrupted.Add(1)
		f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "maintenance", Status: "interrupted"}})})
		return map[string]any{}, true
	}
	f.mu.Unlock()
	if err := s.CancelDream(t.Context(), "dream-cancel"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if interrupted.Load() != 1 || s.childRuns["worker"] != "worker-run" {
		t.Fatal("worker was affected")
	}
	delete(s.childRuns, "worker") // fixture teardown owns only its normal thread.
}

func TestLocalHistoryDoesNotTraversePastRuntimeSessions(t *testing.T) {
	s, f := sessionPair(t, "normal")
	s.mu.Lock()
	s.binding.PastThreads = []string{"past-thread"}
	s.lastTurn = "current-turn"
	s.runs["current-turn"] = "completed"
	s.update()
	s.mu.Unlock()
	f.mu.Lock()
	calls := len(f.pageCalls)
	f.mu.Unlock()
	if err := s.LoadEarlier(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pageCalls) != calls || s.ConversationState().Turn != "current-turn" {
		t.Fatal("IM history affected native session")
	}
}

func TestRenewalRechecksActivityAfterNativeCreate(t *testing.T) {
	s, f := sessionPair(t, "normal")
	s.mu.Lock()
	s.binding.Dreams = map[string]dreamRecord{"dream": {Thread: s.binding.ThreadID}}
	s.binding.Scheduled["dream"] = "dream-turn"
	s.lastTurn, s.runs["dream-turn"] = "dream-turn", "completed"
	s.mu.Unlock()
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method != "thread/start" {
			return nil, false
		}
		s.mu.Lock()
		s.applyTurn(nativeTurn{ID: "external-input", Status: "inProgress"}, false)
		s.update()
		s.mu.Unlock()
		return map[string]any{"thread": nativeThread{ID: "unused-empty-thread"}, "model": "native-default"}, true
	}
	f.mu.Unlock()
	if err := s.RenewConversation(t.Context(), "dream", "thread-native"); err == nil {
		t.Fatal("rotated across new activity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding.ThreadID != "thread-native" || s.run != "external-input" {
		t.Fatal("lost active source conversation")
	}
	s.run = "" // Synthetic external run has no fixture producer to interrupt.
}
