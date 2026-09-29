package codex

import "testing"

func TestRecoveredCommentaryNeedsLiveEvidenceToBecomeStreaming(t *testing.T) {
	s, _ := sessionPair(t, "normal")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyTurn(nativeTurn{ID: "recovered", Status: "inProgress", Items: []nativeItem{
		{ID: "commentary", Type: "agentMessage", Text: "I will read the file."},
		{ID: "tool", Type: "commandExecution", Status: "inProgress"},
	}}, true)
	status := func(id string) string { return s.state.Items[s.items[opaque("recovered", id)]].Status }
	if status("commentary") != "" || status("tool") != "inProgress" {
		t.Fatal("recovery invented streaming or lost the running tool")
	}
	emit := func(method string, fields map[string]any) {
		fields["threadId"], fields["turnId"] = s.binding.ThreadID, "recovered"
		s.applyEvent(Notification{Method: method, Params: raw(fields)})
	}
	emit("item/agentMessage/delta", map[string]any{"itemId": "commentary", "delta": " More."})
	if status("commentary") != "inProgress" {
		t.Fatal("live delta did not resume streaming")
	}
	emit("item/completed", map[string]any{"item": nativeItem{ID: "commentary", Type: "agentMessage", Text: "Done."}})
	emit("item/agentMessage/delta", map[string]any{"itemId": "commentary", "delta": "late"})
	if status("commentary") != "completed" || status("tool") != "inProgress" {
		t.Fatal("completion or concurrent tool was lost")
	}
	emit("item/started", map[string]any{"item": nativeItem{ID: "fresh", Type: "agentMessage", Text: "Answer"}})
	if status("fresh") != "inProgress" {
		t.Fatal("live start did not mark streaming")
	}
}
