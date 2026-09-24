package desktop

import "sync"

type nativeEvent struct {
	kind        int
	x, y, scale float64
	text        string
}

// Native callbacks cannot wait on a service operation that calls back into
// AppKit. Queue completed gestures in arrival order without holding this mutex
// during processing. High-frequency drag/slider previews never enter the queue.
type nativeEventQueue struct {
	mu      sync.Mutex
	pending []nativeEvent
	wake    chan struct{}
	stopped bool
}

func newNativeEventQueue(consume func(nativeEvent)) *nativeEventQueue {
	q := &nativeEventQueue{wake: make(chan struct{}, 1)}
	go func() {
		for range q.wake {
			for {
				q.mu.Lock()
				if q.stopped {
					q.mu.Unlock()
					return
				}
				if len(q.pending) == 0 {
					q.mu.Unlock()
					break
				}
				e := q.pending[0]
				q.pending = q.pending[1:]
				q.mu.Unlock()
				consume(e)
			}
		}
	}()
	return q
}

func (q *nativeEventQueue) push(e nativeEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return
	}
	q.pending = append(q.pending, e)
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *nativeEventQueue) stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return
	}
	q.stopped = true
	q.pending = nil
	close(q.wake)
}
