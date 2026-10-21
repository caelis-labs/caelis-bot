package desktop

import "testing"

func TestIndependentSurfacePresentation(t *testing.T) {
	var p surfacePresentations
	chat, settings := p.prepare("chat"), p.prepare("settings")
	if !p.accept("chat", chat) || !p.accept("settings", settings) {
		t.Fatal("one window cancelled the other")
	}
	first, second := p.prepare("settings"), p.prepare("settings")
	if p.accept("settings", first) || !p.accept("settings", second) || p.accept("settings", second) {
		t.Fatal("stale or duplicate acknowledgement")
	}
	chat, settings = p.prepare("chat"), p.prepare("settings")
	p.cancel("settings")
	if p.accept("settings", settings) || !p.accept("chat", chat) {
		t.Fatal("closing settings affected chat")
	}
}
