package caelis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func proseDelivery(id int) wire.SessionFeedDelivery {
	update := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"a"}}`)
	return wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", NextCursor: pointer(fmt.Sprint(id)), Events: []wire.Envelope{{
		Kind: "session/update", EventId: pointer(fmt.Sprint(id)), TurnId: pointer("turn"), Update: &update,
		Delivery: wire.Delivery{Mode: wire.DeliveryModeTransient},
	}}}
}

func TestTransientProjectionCheckpointReplaysWithoutLostOrDuplicateText(t *testing.T) {
	s := New(Options{Directory: filepath.Join(t.TempDir(), "private")})
	v := &view{State: wire.SessionState{SessionId: "main"}, Seen: map[string]bool{}}
	s.state.Views["main"] = v
	start := time.Now()
	apply := func(d wire.SessionFeedDelivery, at time.Time) {
		t.Helper()
		for _, e := range d.Events {
			applyEnvelope(v, e)
		}
		v.Cursor = value(d.NextCursor)
		if err := s.publishProjectionLocked(d, at); err != nil {
			t.Fatal(err)
		}
	}
	apply(proseDelivery(1), start)
	first, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	revision := s.revision
	for i := 2; i <= 201; i++ {
		apply(proseDelivery(i), start.Add(10*time.Millisecond))
	}
	current, _ := os.ReadFile(s.path)
	if !bytes.Equal(first, current) || s.revision != revision || len(v.Items[0].Text) != 201 {
		t.Fatal("transient burst rewrote storage/published each token, or lost live text")
	}
	// Simulate a crash between checkpoints, then exact replay (including the
	// checkpoint event). Cursor and Seen must describe the same durable prefix.
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	r := restored.Views["main"]
	if r.Cursor != "1" || r.Items[0].Text != "a" {
		t.Fatal("cursor advanced without its durable projection")
	}
	for i := 1; i <= 201; i++ {
		applyEnvelope(r, proseDelivery(i).Events[0])
	}
	if r.Items[0].Text != v.Items[0].Text {
		t.Fatal("recovery lost or duplicated streamed text")
	}
	// A time checkpoint flushes the complete prefix, while the terminal fact
	// must flush immediately even one millisecond later.
	apply(proseDelivery(202), start.Add(time.Second))
	terminal := wire.SessionFeedDelivery{Kind: "append_page", NextCursor: pointer("terminal"), Events: []wire.Envelope{{
		Kind: "caelis/lifecycle", TurnId: pointer("turn"), Lifecycle: &wire.LifecycleEvent{State: "completed"},
		Delivery: wire.Delivery{Mode: wire.DeliveryModeCanonical},
	}}}
	apply(terminal, start.Add(time.Second+time.Millisecond))
	restored, err = loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	r = restored.Views["main"]
	if r.Cursor != "terminal" || len(r.Items[0].Text) != 202 || r.Items[0].Status != "completed" || value(r.State.Run.Active) {
		t.Fatal("terminal result was deferred behind the prose checkpoint")
	}
}

func TestProjectionNeverDefersAuthorityOrFinalEvents(t *testing.T) {
	base := proseDelivery(1)
	if !transientProse(base) {
		t.Fatal("non-final transient text was not recognized")
	}
	for name, change := range map[string]func(*wire.SessionFeedDelivery){
		"canonical":  func(d *wire.SessionFeedDelivery) { d.Events[0].Delivery.Mode = wire.DeliveryModeCanonical },
		"final":      func(d *wire.SessionFeedDelivery) { d.Events[0].Final = pointer(true) },
		"permission": func(d *wire.SessionFeedDelivery) { d.Events[0].ApprovalRequestId = pointer("approval") },
		"tool": func(d *wire.SessionFeedDelivery) {
			u := json.RawMessage(`{"sessionUpdate":"tool_call_update","toolCallId":"call","status":"completed"}`)
			d.Events[0].Update = &u
		},
		"sync":        func(d *wire.SessionFeedDelivery) { d.Kind = "sync" },
		"replacement": func(d *wire.SessionFeedDelivery) { d.Kind = "replace_end" },
	} {
		t.Run(name, func(t *testing.T) {
			d := clone(base)
			change(&d)
			if transientProse(d) {
				t.Fatal("authority or stream boundary was treated as discardable prose")
			}
		})
	}
}
