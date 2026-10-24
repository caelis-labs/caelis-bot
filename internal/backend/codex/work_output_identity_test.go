package codex

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWorkerLiveDeltasJoinOnlyExactNativeHistoryMessages(t *testing.T) {
	s, f, _, manager := taskPair(t)
	sendSynthetic(t, s, "output-identity-parent")
	task := newTask(t, manager, "output-identity-start")
	thread, run := workerNativeTarget(t, s, task.ID)
	outputs := func(v api.WorkObservation) []api.Item {
		out := []api.Item{}
		for _, item := range v.Items {
			if item.Kind == "assistant" {
				out = append(out, item)
			}
		}
		return out
	}
	for _, id := range []string{"message-one", "message-two"} {
		f.emit(wireMessage{Method: "item/agentMessage/delta", Params: raw(map[string]any{"threadId": thread, "turnId": run, "itemId": id, "delta": "identical native body"})})
	}
	awaitState(t, s, func(api.Snapshot) bool {
		v, _ := s.ObserveWork(testContext(t), task.ID)
		return len(outputs(v)) == 2
	})
	observed, err := s.ObserveWork(testContext(t), task.ID)
	observed.Items = outputs(observed)
	if err != nil || observed.Items[0].HistoryID == observed.Items[1].HistoryID {
		t.Fatal("same text merged native identities", observed.Items, err)
	}
	for i, id := range []string{"message-one", "message-two"} {
		if observed.Items[i].HistoryID != "msg-"+opaque(thread, run, id) || observed.Items[i].Text != "identical native body" {
			t.Fatal("delta lacks exact native history identity", observed.Items[i])
		}
		f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": thread, "turnId": run, "item": nativeItem{ID: id, Type: "agentMessage", Text: "identical native body"}})})
	}
	awaitState(t, s, func(api.Snapshot) bool {
		v, _ := s.ObserveWork(testContext(t), task.ID)
		v.Items = outputs(v)
		return len(v.Items) == 2 && v.Items[0].Status == "completed" && v.Items[1].Status == "completed"
	})
	f.mu.Lock()
	native := f.workers[thread]
	native.Turns = []nativeTurn{{ID: run, Status: "completed", Items: []nativeItem{{ID: "message-one", Type: "agentMessage", Text: "identical native body"}, {ID: "message-two", Type: "agentMessage", Text: "identical native body"}}}}
	f.workers[thread] = native
	f.mu.Unlock()
	history, err := s.ReadWorkConversation(testContext(t), task.ID)
	if err != nil || len(history) != 2 {
		t.Fatal("native history", history, err)
	}
	for i := range history {
		if history[i].ID != observed.Items[i].HistoryID {
			t.Fatal("live item joined a different original message", history[i], observed.Items[i])
		}
	}
	f.mu.Lock()
	prior := f.handle
	f.handle = func(in wireMessage) (any, bool) {
		if in.Method == "thread/turns/list" {
			return map[string]any{"data": nil}, true
		}
		return prior(in)
	}
	f.mu.Unlock()
	if _, err := s.ReadWorkConversation(testContext(t), task.ID); err == nil {
		t.Fatal("unreadable history became empty success")
	}
	observed, err = s.ObserveWork(testContext(t), task.ID)
	observed.Items = outputs(observed)
	if err != nil || len(observed.Items) != 2 || observed.Items[0].Status != "completed" || observed.Items[1].Status != "completed" {
		t.Fatal("history failure erased completed live output", observed.Items, err)
	}
}
