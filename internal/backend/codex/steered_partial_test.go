package codex

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func assistantByID(t *testing.T, view api.Snapshot, turn, id string) api.Item {
	t.Helper()
	for _, item := range view.Items {
		if item.ID == opaque(turn, id) && item.Kind == "assistant" {
			return item
		}
	}
	t.Fatalf("missing assistant item %s in %v", id, view.Items)
	return api.Item{}
}

// Mirrors app-server notification order without starting a model or a tool.
// Steering is accepted into the same turn while an earlier agentMessage is open.
func TestSteeredPartialKeepsTextAndFollowingCompletion(t *testing.T) {
	s, f := sessionPair(t, "hold")
	first := sendSynthetic(t, s, "first-client-id")
	if first.Outcome != "accepted" {
		t.Fatal(first)
	}
	partial := "Core #111 的流终止修"
	emit := func(method string, fields map[string]any) {
		fields["threadId"], fields["turnId"] = "thread-native", "run-native"
		f.emit(wireMessage{Method: method, Params: raw(fields)})
	}
	emit("item/started", map[string]any{"item": nativeItem{ID: "first-user", Type: "userMessage", ClientID: first.ID}})
	emit("item/started", map[string]any{"item": nativeItem{ID: "partial", Type: "agentMessage"}})
	emit("item/agentMessage/delta", map[string]any{"itemId": "partial", "delta": partial})
	awaitState(t, s, func(v api.Snapshot) bool {
		for _, item := range v.Items {
			if item.ID == opaque("run-native", "partial") && item.Text == partial {
				return true
			}
		}
		return false
	})
	steer := sendSynthetic(t, s, "steer-client-id")
	if steer.Outcome != "accepted" || steer.ID == first.ID {
		t.Fatal(first, steer)
	}
	emit("item/completed", map[string]any{"item": nativeItem{ID: "steer-user", Type: "userMessage", ClientID: steer.ID}})
	emit("item/completed", map[string]any{"item": nativeItem{ID: "following", Type: "agentMessage", Text: "Full response after steering."}})
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "run-native", Status: "completed"}})})
	view := awaitState(t, s, func(v api.Snapshot) bool {
		return v.Phase == "completed" && assistantByID(t, v, "run-native", "partial").Status == "incomplete"
	})
	if old := assistantByID(t, view, "run-native", "partial"); old.Text != partial || old.Status != "incomplete" {
		t.Fatal(old)
	}
	if next := assistantByID(t, view, "run-native", "following"); next.Text != "Full response after steering." || next.Status != "completed" {
		t.Fatal(next)
	}
	if !view.CanSend || view.CanSteer || view.CurrentTurn != "" {
		t.Fatal("turn outcome was confused with item finality", view.Phase, view.CurrentTurn)
	}
	f.mu.Lock()
	starts := f.starts
	f.mu.Unlock()
	if starts != 1 {
		t.Fatal("steering replayed turn/start", starts)
	}
	// A terminal turn fences late deltas and duplicate terminal notifications.
	emit("item/agentMessage/delta", map[string]any{"itemId": "partial", "delta": " invented tail"})
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "run-native", Status: "completed"}})})
	view = awaitState(t, s, func(v api.Snapshot) bool { return v.Revision >= view.Revision+2 })
	if old := assistantByID(t, view, "run-native", "partial"); old.Text != partial || old.Status != "incomplete" {
		t.Fatal("late event changed old output", old)
	}
	// A subsequent turn is independent of the old terminal generation.
	f.emit(wireMessage{Method: "turn/started", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "new-run", Status: "inProgress"}})})
	emit("item/agentMessage/delta", map[string]any{"itemId": "partial", "delta": " stale generation"})
	f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "new-run", "item": nativeItem{ID: "new-answer", Type: "agentMessage", Text: "new turn answer"}})})
	view = awaitState(t, s, func(v api.Snapshot) bool {
		return v.CurrentTurn == opaque("new-run") && v.Phase == "working" && len(v.Items) >= 5
	})
	if old := assistantByID(t, view, "run-native", "partial"); old.Text != partial || old.Status != "incomplete" {
		t.Fatal("new turn or stale delta changed old output", old)
	}
	if next := assistantByID(t, view, "new-run", "new-answer"); next.Status != "completed" {
		t.Fatal("new generation did not complete its own item", next)
	}

	// A late native item/completed may correct this exact identity, but it
	// cannot invent a new message or erase bytes the UI already received.
	emit("item/completed", map[string]any{"item": nativeItem{ID: "unknown", Type: "agentMessage", Text: "must not appear"}})
	emit("item/completed", map[string]any{"item": nativeItem{ID: "partial", Type: "agentMessage", Text: "Core #111"}})
	view = awaitState(t, s, func(v api.Snapshot) bool { return assistantByID(t, v, "run-native", "partial").Status == "completed" })
	if old := assistantByID(t, view, "run-native", "partial"); old.Text != partial {
		t.Fatal("late completion erased received text", old)
	}
	for _, item := range view.Items {
		if item.Kind == "assistant" && item.Text == "must not appear" {
			t.Fatal("late unknown item escaped terminal fence")
		}
	}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "new-run", Status: "completed"}})})
	view = awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" && v.CurrentTurn == "" })
	if assistantByID(t, view, "new-run", "new-answer").Text != "new turn answer" {
		t.Fatal("later turn was affected by old completion")
	}
}

func TestTerminalHistoryKeepsUncompletedAssistantAndReceivedText(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "original")
	f.emit(wireMessage{Method: "item/agentMessage/delta", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "itemId": "partial", "delta": "received prefix"})})
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "run-native", Status: "completed"}})})
	awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	f.mu.Lock()
	f.history = []nativeTurn{{ID: "run-native", Status: "completed", Items: []nativeItem{{ID: "partial", Type: "agentMessage", Text: "received"}, {ID: "following", Type: "agentMessage", Text: "stored complete reply"}}}}
	f.mu.Unlock()
	s.mu.Lock()
	s.binding.Pending = &pendingSubmission{ID: "pending-reconcile"}
	s.state.Phase = "unknown"
	s.update()
	s.mu.Unlock()
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	view := s.Snapshot()
	if old := assistantByID(t, view, "run-native", "partial"); old.Text != "received prefix" || old.Status != "incomplete" {
		t.Fatal("reconnect lost open item", old)
	}
	if next := assistantByID(t, view, "run-native", "following"); next.Status != "completed" {
		t.Fatal("history omitted completed item", next)
	}
}
