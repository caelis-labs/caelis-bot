package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func workerOutputTool(event, tool string) wire.Envelope {
	update, _ := json.Marshal(map[string]any{"sessionUpdate": "tool_call", "toolCallId": tool, "name": "Read", "status": "completed", "kind": "read"})
	return wire.Envelope{SessionId: pointer("work"), TurnId: pointer("turn"), EventId: &event, Kind: "session/update", Final: pointer(true), Update: pointer(json.RawMessage(update))}
}

func workerOutputUnavailableHistoryFixture(t *testing.T) *Session {
	t.Helper()
	head := nativeWorkHead("turn")
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/state") {
			writeFixture(w, head)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	addServiceWorker(s, "task", "work", head)
	return s
}

func TestWorkerOutputWindowKeepsLatestTextAfterOldToolActivities(t *testing.T) {
	s := workerOutputUnavailableHistoryFixture(t)
	for i := range 200 {
		feedWorkerOutput(s, []wire.Envelope{workerOutputTool(fmt.Sprintf("tool-event-%03d", i), fmt.Sprintf("tool-%03d", i))})
	}
	feedWorkerOutput(s, []wire.Envelope{workerOutputMessage("latest-delta", "latest-message", "latest unfinished output", false)})
	for _, observer := range []interface {
		ObserveWork(context.Context, string) (api.WorkObservation, error)
	}{s, &WorkOwner{engine: s}} {
		out, err := observer.ObserveWork(t.Context(), "task")
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(out.Items, func(item api.Item) bool { return item.Text == "latest unfinished output" })
		if index < 0 || index != len(out.Items)-1 || out.Items[index].Status != "inProgress" || out.Items[index].HistoryID != nativeWorkerMessageID("work", "turn", "assistant", "latest-message") {
			t.Fatal("old activities displaced latest original output", len(out.Items), index)
		}
		if len(out.Items) != 200 || out.Items[0].ID != "turn/activity/tool-001" {
			t.Fatal("bound did not evict the oldest display item", len(out.Items), out.Items[0].ID)
		}
	}
	if _, err := s.ReadWorkConversation(t.Context(), "task"); err == nil {
		t.Fatal("history fixture unexpectedly readable")
	}
	out, err := s.ObserveWork(t.Context(), "task")
	if err != nil || len(out.Items) == 0 || out.Items[len(out.Items)-1].Text != "latest unfinished output" {
		t.Fatal("history failure erased latest live output", err)
	}
}

func TestWorkerOutputWindowRetainsLegacyPlainItemsAcrossEmptyProjection(t *testing.T) {
	s := workerOutputUnavailableHistoryFixture(t)
	legacy := []api.Item{
		{ID: "legacy-user", Kind: "user", Text: "legacy accepted input", Status: "completed"},
		{ID: "legacy-assistant", Kind: "assistant", Text: "legacy observed result", Status: "completed"},
	}
	s.state.Views["work"].Items = clone(legacy)
	assertLegacy := func(s *Session) {
		t.Helper()
		for _, observer := range []interface {
			ObserveWork(context.Context, string) (api.WorkObservation, error)
		}{s, &WorkOwner{engine: s}} {
			out, err := observer.ObserveWork(t.Context(), "task")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range legacy {
				index := slices.IndexFunc(out.Items, func(item api.Item) bool { return item.ID == want.ID })
				if index < 0 || out.Items[index].Text != want.Text || out.Items[index].HistoryID != "" {
					t.Fatal("empty or partial projection erased unproven legacy output", want.ID, len(out.Items))
				}
			}
		}
	}
	assertLegacy(s)
	feedWorkerOutput(s, []wire.Envelope{{SessionId: pointer("work"), TurnId: pointer("turn"), EventId: pointer("non-text-first"), Kind: "caelis/lifecycle", Lifecycle: &wire.LifecycleEvent{State: "running"}}})
	assertLegacy(s)
	feedWorkerOutput(s, []wire.Envelope{workerOutputMessage("later-output", "native-later", "new native output", false)})
	assertLegacy(s)
	if _, err := s.ReadWorkConversation(t.Context(), "task"); err == nil {
		t.Fatal("history fixture unexpectedly readable")
	}
	assertLegacy(s)
	s.mu.Lock()
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	reopened := New(Options{Directory: filepath.Dir(s.path)})
	reopened.client, reopened.connected = s.client, true
	assertLegacy(reopened)
}

func TestWorkerOutputWindowPreservesObservedTextAndToolOrder(t *testing.T) {
	s := workerOutputUnavailableHistoryFixture(t)
	first := workerOutputMessage("first-output", "native-one", "identical body", true)
	second := workerOutputMessage("second-output", "native-two", "identical body", false)
	feedWorkerOutput(s, []wire.Envelope{first, workerOutputTool("tool-between", "between"), second})
	// A late completion/update of the existing tool must not make old activity
	// appear newer than the latest original assistant message.
	late := workerOutputTool("late-tool-update", "between")
	var update map[string]any
	_ = json.Unmarshal(*late.Update, &update)
	update["sessionUpdate"] = "tool_call_update"
	data, _ := json.Marshal(update)
	late.Update = pointer(json.RawMessage(data))
	feedWorkerOutput(s, []wire.Envelope{late})
	out, err := s.ObserveWork(t.Context(), "task")
	want := []string{"worker-live:" + nativeWorkerMessageID("work", "turn", "assistant", "native-one"), "turn/activity/between", "worker-live:" + nativeWorkerMessageID("work", "turn", "assistant", "native-two")}
	if err != nil || !reflect.DeepEqual(want, itemIDs(out.Items)) {
		t.Fatal("category concatenation replaced observed ordering", itemIDs(out.Items), err)
	}
	s.mu.Lock()
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	reopened := New(Options{Directory: filepath.Dir(s.path)})
	reopened.client, reopened.connected = s.client, true
	out, err = reopened.ObserveWork(t.Context(), "task")
	if err != nil || !reflect.DeepEqual(want, itemIDs(out.Items)) {
		t.Fatal("restart lost display order", itemIDs(out.Items), err)
	}
}

func TestWorkerOutputWindowReplacementCannotEraseUnprovenLegacy(t *testing.T) {
	head := nativeWorkHead("turn")
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/state") {
			writeFixture(w, head)
			return
		}
		workerHistoryFrames(w, []wire.Envelope{workerOutputTool("replacement-tool", "new-tool"), workerOutputMessage("replacement-output", "native-replacement", "same legacy body", true)}, "")
	})
	addServiceWorker(s, "task", "work", head)
	// Even an old grouped row with the same display ID, turn and prose has no
	// reliable native-message identity to let a new replay discard its bytes.
	legacyID := "turn/assistant/native-replacement"
	s.state.Views["work"].Items = []api.Item{{ID: legacyID, TurnKey: "turn", Kind: "assistant", Text: "same legacy body", Status: "completed"}}
	feedWorkerOutput(s, []wire.Envelope{workerOutputTool("first-tool", "old-tool")})
	if err := s.watch(t.Context(), s.client, "work", s.state.InstanceID); err == nil {
		t.Fatal("finite replacement fixture unexpectedly kept streaming")
	}
	out, err := s.ObserveWork(t.Context(), "task")
	if err != nil || len(out.Items) != 3 || out.Items[0].ID != legacyID || out.Items[0].HistoryID != "" || out.Items[2].HistoryID == "" || out.Items[0].Text != out.Items[2].Text {
		t.Fatal("replacement deduplicated unproven rows by prose or turn", out.Items, err)
	}
}

func TestWorkerOutputWindowBoundsPresentationMetadataAndKeepsOpaqueQuestions(t *testing.T) {
	s := workerOutputUnavailableHistoryFixture(t)
	for i := range 350 {
		feedWorkerOutput(s, []wire.Envelope{workerOutputTool(fmt.Sprintf("tool-event-%03d", i), fmt.Sprintf("tool-%03d", i)), workerOutputMessage(fmt.Sprintf("message-event-%03d", i), fmt.Sprintf("message-%03d", i), strings.Repeat("x", 4<<10), true)})
	}
	question := api.Item{ID: "original-opaque", Kind: "assistant", AsyncCallID: "original-native-call", AsyncQuestions: []api.AsyncQuestion{{Title: "opaque request", Options: []string{"original-native-option"}}}}
	s.state.Views["work"].Items = append(s.state.Views["work"].Items, question)
	out, err := s.ObserveWork(t.Context(), "task")
	index := slices.IndexFunc(out.Items, func(item api.Item) bool { return item.ID == question.ID })
	if err != nil || index < 0 || out.Items[index].AsyncCallID != question.AsyncCallID || !reflect.DeepEqual(out.Items[index].AsyncQuestions, question.AsyncQuestions) {
		t.Fatal("shared observation lost original opaque question fields", index, err)
	}
	p := s.state.Views["work"].WorkerOutput
	encoded, _ := json.Marshal(p)
	if len(p.Items)+len(p.Legacy) > 200 || len(p.Order) > 400 || len(p.Covered) > 200 || len(encoded) > 550<<10 {
		t.Fatal("display ordering metadata became an unbounded journal", len(p.Items), len(p.Legacy), len(p.Order), len(p.Covered), len(encoded))
	}
	if len(out.Items) > 200 {
		t.Fatal("merged output exceeded shared display window", len(out.Items))
	}
	residents := make([]api.Item, 200)
	for i := range residents {
		residents[i] = api.Item{ID: fmt.Sprintf("replacement-activity-%03d", i), Kind: "activity"}
	}
	next := newWorkerTranscript("work", residents)
	for i := range 200 {
		if err := next.apply(workerOutputMessage(fmt.Sprintf("replacement-event-%03d", i), fmt.Sprintf("replacement-message-%03d", i), strings.Repeat("y", 4<<10), true)); err != nil {
			t.Fatal(err)
		}
		next.bound()
	}
	next.retainMissing(p)
	if len(next.Items)+len(next.Legacy) > 200 || len(next.Order) > 400 || len(next.Covered) > 200 {
		t.Fatal("replacement retained stale ordering metadata", len(next.Items), len(next.Order), len(next.Covered))
	}
}

func TestWorkerOutputWindowLegacyBoundDoesNotReimportOldRowsAsNew(t *testing.T) {
	s := workerOutputUnavailableHistoryFixture(t)
	for i := range 200 {
		s.state.Views["work"].Items = append(s.state.Views["work"].Items, api.Item{ID: fmt.Sprintf("legacy-%03d", i), Kind: "assistant", Text: strings.Repeat("x", 8<<10), Status: "completed"})
	}
	feedWorkerOutput(s, []wire.Envelope{{SessionId: pointer("work"), EventId: pointer("non-text"), Kind: "caelis/lifecycle", Lifecycle: &wire.LifecycleEvent{State: "running"}}})
	for range 5 {
		out, err := s.ObserveWork(t.Context(), "task")
		if err != nil || len(out.Items) == 0 || out.Items[len(out.Items)-1].ID != "legacy-199" {
			t.Fatal("re-observation promoted previously bounded old rows over the latest legacy output", itemIDs(out.Items), err)
		}
	}
}

func itemIDs(items []api.Item) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}
