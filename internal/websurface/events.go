package websurface

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// One bounded observation source serves all tabs. Each stream starts with a
// complete current projection; this is explicitly not a durable event log.
func (s *Server) observe() {
	defer s.workers.Done()
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	var revision uint64
	var status string
	for {
		update := s.services.Backend.ChatSnapshot(revision, status)
		if update.Changed {
			revision, status = update.Snapshot.Revision, update.Snapshot.BotStatus
			data, err := json.Marshal(update)
			if err == nil {
				s.eventMu.Lock()
				s.latest = data
				for subscriber := range s.subscribers {
					select {
					case subscriber <- data:
					default:
						select {
						case <-subscriber:
						default:
						}
						select {
						case subscriber <- data:
						default:
						}
					}
				}
				s.eventMu.Unlock()
			}
		}
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if _, ok := w.(http.Flusher); !ok {
		fail(w, 500, "stream_unavailable")
		return
	}
	stream := make(chan []byte, 1)
	s.eventMu.Lock()
	if len(s.subscribers) >= 32 {
		s.eventMu.Unlock()
		fail(w, 429, "connection_limit")
		return
	}
	s.subscribers[stream] = struct{}{}
	if s.latest != nil {
		stream <- s.latest
	}
	s.eventMu.Unlock()
	defer func() { s.eventMu.Lock(); delete(s.subscribers, stream); s.eventMu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	write := func(data string) error {
		_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, e := fmt.Fprint(w, data)
		if e == nil {
			e = rc.Flush()
		}
		return e
	}
	if write("event: reset\ndata: {}\n\n") != nil {
		return
	}
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case data := <-stream:
			if _, _, ok := s.authenticate(r); !ok {
				return
			}
			if write("event: snapshot\ndata: "+string(data)+"\n\n") != nil {
				return
			}
			s.touchSession(sessionID(r))
		case <-heartbeat.C:
			if _, _, ok := s.authenticate(r); !ok {
				return
			}
			if write(": heartbeat\n\n") != nil {
				return
			}
			s.touchSession(sessionID(r))
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		}
	}
}
func (s *Server) operationStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	v, ok, err := s.lookupOperation(r.PathValue("id"))
	s.mu.Unlock()
	if err != nil {
		fail(w, 503, "receipt_unavailable")
		return
	}
	if !ok || v.Owner != sessionID(r) {
		fail(w, 404, "operation_unavailable")
		return
	}
	body := map[string]any{"state": v.State, "value": v.Value, "error": v.Error}
	if detail := operationDetail(v); detail != "" {
		body["detail"] = detail
	}
	jsonOut(w, 200, body)
}
