package desktop

import "testing"

func TestNativeGesturesStayOrderedWithoutBlockingAppKit(t *testing.T) {
	s, _, store := setup()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	q := newNativeEventQueue(func(e nativeEvent) {
		switch e.kind {
		case 6:
			close(entered)
			<-release // Models a Go operation waiting for the AppKit thread.
			if err := s.resized(e.x, e.y, e.scale); err != nil {
				t.Error(err)
			}
		case 5:
			if err := s.SetVisible(false); err != nil {
				t.Error(err)
			}
		case 7:
			s.shutdown()
			close(done)
		default:
			t.Error("callback processed after stop")
		}
	})
	defer q.stop()
	q.push(nativeEvent{kind: 6, x: 600, y: 300, scale: 1.234567})
	<-entered
	// These enqueues must return without waiting for the in-flight service call.
	q.push(nativeEvent{kind: 5})
	q.push(nativeEvent{kind: 7})
	close(release)
	<-done
	if store.value.Scale != 1.234567 || store.value.Visible || store.saves != 3 {
		t.Fatalf("hide/quit overtook resize: %+v, saves %d", store.value, store.saves)
	}
	q.stop()
	q.push(nativeEvent{kind: 99})
}
