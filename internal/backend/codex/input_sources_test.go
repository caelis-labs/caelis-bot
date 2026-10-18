package codex

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRenewalDoesNotRelabelPriorInputs(t *testing.T) {
	s, f := sessionPair(t, "normal")
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		switch m.Method {
		case "thread/start":
			return map[string]any{"thread": nativeThread{ID: "next-thread"}, "model": "native-default"}, true
		case "turn/start":
			var p struct {
				ID    string        `json:"clientUserMessageId"`
				Input []nativeInput `json:"input"`
			}
			if err := json.Unmarshal(m.Params, &p); err != nil {
				t.Error(err)
			}
			return map[string]any{"turn": nativeTurn{ID: p.ID + "-turn", Status: "completed", Items: []nativeItem{
				{ID: "input", Type: "userMessage", ClientID: p.ID, Content: p.Input},
				{ID: "reply", Type: "agentMessage", Text: "Fixture result"},
			}}}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	if r, err := s.Submit(t.Context(), api.Submission{ID: "old-wx-request", Text: "Fixture request"}, nil); err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	service := backend.NewService(s, nil, nil, nil, nil)
	before := service.Snapshot()
	if len(before.InputSources) != 1 {
		t.Fatal("missing initial input source")
	}
	if r, err := s.SubmitDream(t.Context(), api.Submission{ID: "dream-review", Text: "Fixture maintenance"}); err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	if err := s.RenewConversation(t.Context(), "dream-review", "thread-native"); err != nil {
		t.Fatal(err)
	}
	after := service.Snapshot()
	if before.RuntimeOwner == after.RuntimeOwner {
		t.Fatal("test did not renew native thread")
	}
	if !reflect.DeepEqual(after.InputSources, before.InputSources) {
		t.Fatal("renewal discarded or relabeled original input evidence")
	}
	if len(after.Items) == 0 {
		t.Fatal("renewal removed display history")
	}
	for _, source := range after.InputSources {
		if source.RequestID == "old-wx-request" && source.RuntimeOwner != before.RuntimeOwner {
			t.Fatalf("prior native input relabeled with current owner: before=%s after=%s source=%+v", before.RuntimeOwner, after.RuntimeOwner, source)
		}
	}
}

func TestInputSourcesRequireNativeThreadAndSurviveCacheAndRead(t *testing.T) {
	s, f := sessionPair(t, "")
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"turn": nativeTurn{ID: "unscoped-turn", Status: "completed", Items: []nativeItem{{ID: "unscoped", Type: "userMessage", ClientID: "unscoped-request"}}}})})
	awaitState(t, s, func(v api.Snapshot) bool { return len(v.Items) > 0 })
	if len(s.Snapshot().InputSources) != 0 {
		t.Fatal("notification without native thread gained current scope")
	}
	f.mu.Lock()
	f.history = []nativeTurn{{ID: "recovered-turn", Status: "completed", Items: []nativeItem{{ID: "recovered-user", Type: "userMessage", ClientID: "recovered-request"}}}}
	peer := f.peer
	f.mu.Unlock()
	peer.Close()
	awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "offline" })
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	got := s.Snapshot()
	want := []api.InputSource{{RuntimeOwner: opaque("codex", "thread-native"), TurnKey: opaque("recovered-turn"), RequestID: "recovered-request"}}
	if !reflect.DeepEqual(got.InputSources, want) {
		t.Fatal("scoped native read lost input evidence", got.InputSources)
	}
	s.mu.Lock()
	cached := s.Snapshot()
	s.mu.Unlock()
	if !reflect.DeepEqual(cached.InputSources, want) || !reflect.DeepEqual(s.RecentSnapshot().InputSources, want) {
		t.Fatal("cache/recent lost input evidence")
	}
	cached.InputSources[0].RuntimeOwner = "mutated"
	if !reflect.DeepEqual(s.Snapshot().InputSources, want) {
		t.Fatal("snapshot aliases source cache")
	}
	encoded, err := json.Marshal(got)
	if err != nil || strings.Contains(string(encoded), "InputSources") || strings.Contains(string(encoded), want[0].RuntimeOwner) {
		t.Fatal("private source exposed to renderer")
	}
}
