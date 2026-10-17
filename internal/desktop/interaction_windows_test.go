package desktop

import (
	"encoding/json"
	"testing"
)

func TestWindowsPetInteractionReleaseAndCancellation(t *testing.T) {
	var s windowsPetInteraction
	s.begin()
	s.end()
	if !s.click() || s.Pressing || s.Dragging {
		t.Fatal("ordinary release must retain single-click eligibility")
	}
	s.begin()
	s.move()
	s.end()
	if s.click() || s.Pressing || s.Dragging {
		t.Fatal("a drag returning to its origin must not open chat or keep running")
	}
	s.begin()
	s.cancel()
	s.end()
	if s.click() || s.Pressing || s.Dragging {
		t.Fatal("cancelled press became a delayed click")
	}
	s.begin()
	s.move()
	s.cancel()
	s.end()
	if s.click() || s.Dragging {
		t.Fatal("cancelled native move retained running")
	}
	s.begin()
	s.end()
	if !s.click() {
		t.Fatal("cancellation leaked into the next independent click")
	}
}

func TestWindowsPetContextReadDoesNotAliasNativeSnapshot(t *testing.T) {
	d := &windowsDriver{}
	if string(d.desktopContext()) != "null" {
		t.Fatal("unobserved context must stay unknown")
	}
	d.contextSnapshot.Store(json.RawMessage(`{"revision":1,"interaction":{"dragging":true}}`))
	got := d.desktopContext()
	got[0] = '!'
	if !json.Valid(d.desktopContext()) {
		t.Fatal("a reader mutated the native observation")
	}
}
