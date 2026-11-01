package websurface

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type cacheEpochBlocked struct {
	testEngine
	calls            atomic.Int64
	entered, release chan struct{}
}

func (e *cacheEpochBlocked) Snapshot() api.Snapshot {
	if e.calls.Add(1) == 2 {
		close(e.entered)
		<-e.release
	}
	return api.Snapshot{Revision: 120, Connection: "ready", CanSend: true, Items: []api.Item{{ID: "outgoing", Kind: "assistant", Text: "outgoing"}}}
}

// Produce the real Service/HTTP/SSE sequence consumed by the browser regression.
// The observer's outgoing read stays blocked while HTTP observes the new binding.
func TestChatResetCacheEpochSequence(t *testing.T) {
	e := &cacheEpochBlocked{entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	defer once.Do(func() { close(e.release) })
	s := testServer(t, e)
	c := pairClient(t, s)
	select {
	case <-e.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("background read did not block")
	}
	replacement := &cacheEpochCurrent{}
	replacement.revision.Store(8)
	s.services.Backend.BindRuntime(backend.RuntimeBinding{Engine: replacement})
	response := c.request("/api/v1/conversation/chat-snapshot", map[string]any{"args": []any{0, "", ""}})
	var current struct {
		Value api.ChatUpdate `json:"value"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &current) != nil {
		t.Fatal("HTTP replacement failed")
	}
	h := httptest.NewServer(s)
	defer h.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", h.URL+"/api/v1/events", nil)
	req.Host = "127.0.0.1:9317"
	req.AddCookie(c.cookie)
	res, err := h.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var cached api.ChatUpdate
	reset := false
	scan := bufio.NewScanner(res.Body)
	for scan.Scan() {
		line := scan.Text()
		if line == "event: reset" {
			reset = true
		}
		if strings.HasPrefix(line, "data: ") && line != "data: {}" {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cached); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if !reset || !cached.Changed || cached.Snapshot.ProcessEpoch == current.Value.Snapshot.ProcessEpoch || cached.Snapshot.Items[0].Text != "outgoing" {
		t.Fatalf("expected stale initial SSE cache: reset=%t cached=%+v current=%+v", reset, cached, current)
	}
	confirmedResponse := c.request("/api/v1/conversation/chat-snapshot", map[string]any{"args": []any{current.Value.Snapshot.Revision, current.Value.Snapshot.BotStatus, current.Value.Snapshot.ProcessEpoch}})
	var confirmed struct {
		Value api.ChatUpdate `json:"value"`
	}
	if confirmedResponse.Code != 200 || json.Unmarshal(confirmedResponse.Body.Bytes(), &confirmed) != nil || confirmed.Value.Changed {
		t.Fatal("authoritative same-epoch read was not unchanged")
	}
	replacement.revision.Store(9)
	nextResponse := c.request("/api/v1/conversation/chat-snapshot", map[string]any{"args": []any{current.Value.Snapshot.Revision, current.Value.Snapshot.BotStatus, current.Value.Snapshot.ProcessEpoch}})
	var next struct {
		Value api.ChatUpdate `json:"value"`
	}
	if nextResponse.Code != 200 || json.Unmarshal(nextResponse.Body.Bytes(), &next) != nil || !next.Value.Changed || next.Value.Snapshot.ProcessEpoch != current.Value.Snapshot.ProcessEpoch || next.Value.Snapshot.Revision <= current.Value.Snapshot.Revision {
		t.Fatal("replacement did not advance within its epoch")
	}
	if file := os.Getenv("CAELIS_BOT_TEST_EPOCH_PROOF"); file != "" {
		data, _ := json.MarshalIndent(map[string]any{"http_current": current.Value, "sse_after_reset": cached, "http_progress": next.Value, "http_confirmed": confirmed.Value, "reset_observed": reset}, "", "  ")
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	once.Do(func() { close(e.release) })
	t.Log("Production HTTP returns replacement while reset SSE still sends the outgoing epoch; no writes or Submit")
}

type cacheEpochCurrent struct {
	testEngine
	revision atomic.Uint64
}

func (e *cacheEpochCurrent) Snapshot() api.Snapshot {
	return api.Snapshot{Revision: e.revision.Load(), Connection: "ready", CanSend: true, Items: []api.Item{{ID: "current", Kind: "assistant", Text: "current"}}}
}
