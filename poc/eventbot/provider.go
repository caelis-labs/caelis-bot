package main

// A synthetic, loopback-only Responses provider exercises the real Codex runtime
// without user credentials, model costs, third-party traffic or agent tools.
import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"
)

type provider struct {
	*httptest.Server
	calls atomic.Int64
	delay time.Duration
}

func newProvider(delay time.Duration) *provider {
	p := &provider{delay: delay}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		_, err := io.Copy(io.Discard, io.LimitReader(r.Body, 4<<20))
		if err != nil {
			return
		}
		n := p.calls.Add(1)
		id := fmt.Sprintf("poc-%d", n)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		select {
		case <-time.After(p.delay):
		case <-r.Context().Done():
			return
		}
		item := map[string]any{"id": id + "-message", "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "POC_EVENT_ACK", "annotations": []any{}}}}
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	return p
}
