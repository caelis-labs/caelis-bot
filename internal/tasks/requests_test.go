package tasks

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestRequestLookupSurvivesUnknownAndRestart(t *testing.T) {
	root := t.TempDir()
	f := newRuntime()
	f.unknownStart = true
	m := openFixture(t, root, "codex", f)
	v, e := m.StartTask(t.Context(), input("lost-original-receipt"))
	if e == nil {
		t.Fatal("expected unknown")
	}
	lookup, e := m.ReadTaskRequest(t.Context(), "lost-original-receipt")
	if e != nil || lookup.ID != v.ID || f.starts != 1 {
		t.Fatal("lookup replayed", e)
	}
	m = openFixture(t, root, "codex", f)
	lookup, e = m.ReadTaskRequest(t.Context(), "lost-original-receipt")
	if e != nil || lookup.ID != v.ID {
		t.Fatal("restart lost receipt", e)
	}
	f.complete(v.ID)
	m.ReadTask(t.Context(), v.ID)
	_, e = m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "continue-original", Prompt: "Follow up"})
	if e != nil {
		t.Fatal(e)
	}
	m = openFixture(t, root, "codex", f)
	lookup, e = m.ReadTaskRequest(t.Context(), "continue-original")
	if e != nil || lookup.ID != v.ID {
		t.Fatal("follow-up lookup", e)
	}
	if _, e = m.ReadTaskRequest(t.Context(), "not-submitted-id"); e == nil {
		t.Fatal("missing result invented")
	}
	if f.starts != 1 {
		t.Fatal("lookup executed")
	}
}
