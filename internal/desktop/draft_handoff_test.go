package desktop

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Exercises the host confirmation protocol with the real draft store. The
// request callback stands in for ExecJS and its acknowledgement for Wails IPC.
func TestDraftHandoffWaitsForPeerPersistenceInBothDirections(t *testing.T) {
	host := newService(&memoryStore{value: defaults()})
	store := backend.NewService(nil, nil, nil, nil, nil)
	if err := store.ConfigureDraft(filepath.Join(t.TempDir(), "draft.json")); err != nil {
		t.Fatal(err)
	}
	requests := make(chan struct {
		surface string
		id      uint64
	}, 2)
	host.requestDraftFlush = func(surface string, id uint64) {
		requests <- struct {
			surface string
			id      uint64
		}{surface, id}
	}
	if err := host.RegisterDraftEditor("panel"); err != nil {
		t.Fatal(err)
	}
	if err := host.FlushOtherDraft("panel"); err != nil {
		t.Fatal("an editor that has never loaded cannot hold a draft", err)
	}
	if err := host.RegisterDraftEditor("history"); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ incoming, outgoing, text string }{
		{"history", "panel", "panel replacement"},
		{"panel", "history", "history replacement"},
	} {
		done := make(chan error, 1)
		go func() { done <- host.FlushOtherDraft(step.incoming) }()
		var request struct {
			surface string
			id      uint64
		}
		select {
		case request = <-requests:
		case <-time.After(time.Second):
			t.Fatal("outgoing editor was not requested")
		}
		if request.surface != step.outgoing {
			t.Fatalf("requested %s, wanted %s", request.surface, step.outgoing)
		}
		select {
		case <-done:
			t.Fatal("incoming editor read through an unconfirmed save")
		default:
		}
		prior := store.Draft()
		next, err := store.SaveDraft(api.Draft{Revision: prior.Revision, Text: step.text})
		if err != nil {
			t.Fatal(err)
		}
		host.ConfirmDraftFlush(request.id, true)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := store.Draft(); got.Revision != next.Revision || got.Text != step.text {
			t.Fatalf("handoff read stale draft: %+v", got)
		}
		if _, err := store.SaveDraft(api.Draft{Revision: prior.Revision, Text: "stale replacement"}); err == nil || store.Draft().Text != step.text {
			t.Fatal("stale editor overwrote the confirmed replacement")
		}
	}
	done := make(chan error, 1)
	go func() { done <- host.FlushOtherDraft("history") }()
	request := <-requests
	host.ConfirmDraftFlush(request.id, false)
	if err := <-done; err == nil {
		t.Fatal("failed save confirmed the handoff")
	}
}
