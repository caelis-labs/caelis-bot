package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicitly opt-in: only operate the disposable DTW scene fixture supplied by
// the native acceptance harness. The model endpoint remains local/synthetic.
func verifyDesktopWorldFixture(t *testing.T, root string, call func(map[string]any, any) error) {
	t.Helper()
	type state struct {
		PID    int `json:"pid"`
		Clicks int `json:"clicks"`
	}
	read := func() state {
		t.Helper()
		b, e := os.ReadFile(filepath.Join(root, "state.json"))
		var s state
		if e != nil || json.Unmarshal(b, &s) != nil || s.PID <= 0 {
			t.Fatal("invalid native fixture", e)
		}
		return s
	}
	before := read()
	type reply struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			MIME string `json:"mimeType"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	invoke := func(args map[string]any) reply {
		t.Helper()
		var r reply
		var raw json.RawMessage
		e := call(args, &raw)
		if e == nil {
			e = json.Unmarshal(raw, &r)
		}
		if e != nil || r.IsError || len(r.Content) == 0 {
			t.Fatalf("native MCP call: %v; %+v", e, r)
		}
		return r
	}
	complete := func(args map[string]any) string {
		t.Helper()
		r := invoke(args)
		text := r.Content[0].Text
		fields := strings.Fields(text)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "id=") {
			t.Fatal("missing native receipt", text)
		}
		id := strings.TrimPrefix(fields[0], "id=")
		deadline := time.Now().Add(15 * time.Second)
		for strings.Contains(text, "status=running") && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
			text = invoke(map[string]any{"operation": "result", "id": id}).Content[0].Text
		}
		if !strings.Contains(text, "status=completed") {
			t.Fatal("native operation failed", text)
		}
		return id
	}
	// Codex strips image bytes from this inspection API; actual PNG payload
	// acceptance is performed through the Core loader and direct final MCP.
	image := func(id string) {
		t.Helper()
		r := invoke(map[string]any{"operation": "result", "id": id, "part": "image", "image_index": 0})
		for _, c := range r.Content {
			if c.Type == "image" && c.MIME == "image/png" {
				return
			}
		}
		t.Fatal("PNG image envelope absent from Codex MCP result")
	}
	id := complete(map[string]any{"operation": "app", "pid": before.PID})
	image(id)
	var raw strings.Builder
	offset := 0
	for {
		r := invoke(map[string]any{"operation": "result", "id": id, "part": "structured", "offset": offset})
		var page struct {
			Page struct {
				Data string `json:"data"`
				Next *int   `json:"next_offset"`
			} `json:"page"`
		}
		if json.Unmarshal([]byte(r.Content[0].Text), &page) != nil {
			t.Fatal("invalid structured page")
		}
		raw.WriteString(page.Page.Data)
		if page.Page.Next == nil {
			break
		}
		offset = *page.Page.Next
	}
	var observed struct {
		ID     string `json:"observation_id"`
		Frames []struct {
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"frames"`
	}
	if json.Unmarshal([]byte(raw.String()), &observed) != nil || observed.ID == "" || len(observed.Frames) == 0 {
		t.Fatal("invalid fixture observation")
	}
	// The fixture's Count click button has a fixed, measured frame-relative center.
	frame := observed.Frames[0]
	complete(map[string]any{"operation": "click", "observation_id": observed.ID, "image_index": 0, "x": frame.Width * .5, "y": frame.Height * .55})
	deadline := time.Now().Add(3 * time.Second)
	for read().Clicks == before.Clicks && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if got := read(); got.PID != before.PID || got.Clicks != before.Clicks+1 {
		t.Fatal("independent fixture readback", before, got)
	}
	image(complete(map[string]any{"operation": "app", "pid": before.PID}))
	t.Log("real Codex App Server + Bot bridge: PNG image envelopes, one click, independent effect readback, fresh observation passed")
}
