package desktop

import "encoding/json"

// Native extensions stay local to the desktop driver. They never expose a
// protocol client, native task ID, or backend execution authority.
type panelMenuDriver interface{ panelMenu(height, activation int) }
type bubbleDriver interface {
	expandBubble(bool)
	bubbleHeight(int)
}
type activityDriver interface{ activity(string) }
type gestureDriver interface{ gesture(string) }

// Missing desktop context is unknown (JSON null), never a fabricated screen.
type contextDriver interface{ desktopContext() json.RawMessage }

// Props are optional presentation: missing support rejects launch with false.
type propDriver interface {
	planeReady(bool)
	launchPlane(string, float64, float64) bool
	finishPlane(string, bool)
}
