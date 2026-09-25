package codex

import (
	"path/filepath"
	"testing"
)

func TestWorkerCompletionOrdering(t *testing.T) {
	for _, completionNoticeFirst := range []bool{true, false} {
		name := "child_terminal_then_parent_notice"
		if completionNoticeFirst {
			name = "parent_notice_then_child_terminal"
		}
		t.Run(name, func(t *testing.T) {
			s := NewSession(SessionOptions{StateFile: filepath.Join(t.TempDir(), "binding.json")})
			defer s.cancelLife()
			s.binding.ThreadID = "root"
			s.state.Connection = "ready"
			s.children["child"] = true
			s.childWatching["child"] = true
			s.childRuns["child"] = "child-turn"
			s.applyTurn(nativeTurn{ID: "root-turn", Status: "inProgress"}, false)
			childTerminal := Notification{Method: "turn/completed", Params: raw(map[string]any{"threadId": "child", "turn": nativeTurn{ID: "child-turn", Status: "completed"}})}
			parentNotice := Notification{Method: "item/completed", Params: raw(map[string]any{"threadId": "root", "turnId": "root-turn", "item": nativeItem{ID: "completion-notice", Type: "subAgentActivity", AgentThreadID: "child", ActivityKind: "completed"}})}
			events := []Notification{childTerminal, parentNotice}
			if completionNoticeFirst {
				events = []Notification{parentNotice, childTerminal}
			}
			events = append(events, Notification{Method: "turn/completed", Params: raw(map[string]any{"threadId": "root", "turn": nativeTurn{ID: "root-turn", Status: "completed"}})})
			for _, event := range events {
				s.applyEvent(event)
				s.update()
			}
			v := s.Snapshot()
			t.Logf("root=%q childTerminal=%v activeChildren=%v phase=%s canSend=%v canInterrupt=%v", s.run, s.childTerminals[opaque("child", "child-turn")], s.childRuns, v.Phase, v.CanSend, v.CanInterrupt)
			if v.Phase != "completed" || !v.CanSend || v.CanInterrupt {
				t.Fatal("completed root and child should leave the Bot ready for input")
			}
			// Duplicate notices and a late start for the completed turn cannot
			// revive it. A genuinely new turn must still make the Bot busy.
			for range 2 {
				s.applyEvent(parentNotice)
			}
			s.childEvent(Notification{Method: "turn/started", Params: raw(map[string]any{"turn": nativeTurn{ID: "child-turn", Status: "inProgress"}})}, "child", "child-turn")
			s.update()
			if !s.Snapshot().CanSend {
				t.Fatal("terminal turn revived")
			}
			s.childEvent(Notification{Method: "turn/started", Params: raw(map[string]any{"turn": nativeTurn{ID: "next-turn", Status: "inProgress"}})}, "child", "next-turn")
			s.update()
			if s.Snapshot().CanSend || !s.Snapshot().CanInterrupt {
				t.Fatal("new native turn ignored")
			}
		})
	}
}
