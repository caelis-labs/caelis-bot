package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"strings"
	"testing"
	"unicode/utf8"
)

func collectBoundedJSON(t *testing.T, read func(string) api.ToolResult) string {
	t.Helper()
	var out strings.Builder
	cursor, digest := "", ""
	for range 1000 {
		result := read(cursor)
		if result.IsError || len(encoded(result.StructuredContent)) > 31<<10 {
			t.Fatal("bounded read failed", result.StructuredContent)
		}
		data := result.StructuredContent["data"].(map[string]any)
		piece := data["json"].(string)
		if !utf8.ValidString(piece) || data["offset"] != out.Len() {
			t.Fatal("invalid original chunk")
		}
		if digest == "" {
			digest = data["sha256"].(string)
		}
		if digest != data["sha256"] {
			t.Fatal("mixed revisions")
		}
		out.WriteString(piece)
		cursor, _ = data["nextCursor"].(string)
		if cursor == "" {
			if data["complete"] != true || interactionFingerprint(json.RawMessage(out.String())) != digest {
				t.Fatal("missing original bytes")
			}
			return out.String()
		}
	}
	t.Fatal("read did not finish")
	return ""
}

func TestLargeReadHasLosslessExecutableContinuation(t *testing.T) {
	r, _, _ := fixture(t)
	original := map[string]any{"text": strings.Repeat("原文<&>\n", 10000)}
	got := collectBoundedJSON(t, func(cursor string) api.ToolResult {
		return boundedData(map[string]any{"ok": true, "data": original}, map[string]any{"cursor": cursor})
	})
	if got != string(encoded(original)) {
		t.Fatal("original was rewritten")
	}
	first := boundedData(map[string]any{"ok": true, "data": original}, nil)
	changed := boundedData(map[string]any{"ok": true, "data": map[string]string{"text": "changed"}}, map[string]any{"cursor": first.StructuredContent["data"].(map[string]any)["nextCursor"]})
	if !changed.IsError {
		t.Fatal("revision cursor reused")
	}
	// Calendar list no longer serializes every saved prompt; exact read remains available.
	saveReminder(t, r, "large")
	r.mu.Lock()
	r.state.Schedules[0].Prompt = strings.Repeat("<&>", 4000)
	saved := r.state.Schedules[0]
	r.mu.Unlock()
	list := invokeCompact(t, r, "bot_schedule", `{"request":{"type":"list","kind":"calendar"}}`)
	if len(encoded(list.StructuredContent)) > 12<<10 {
		t.Fatal("calendar list repeated full prompt")
	}
	got = collectBoundedJSON(t, func(cursor string) api.ToolResult {
		return r.CallTool(t.Context(), "bot_schedule", encoded(map[string]any{"request": map[string]any{"type": "read", "handle": "calendar:large", "cursor": cursor}}))
	})
	if got != string(encoded(saved)) {
		t.Fatal("calendar read lost saved context")
	}
}

func TestSummaryHistoryPagesCannotOverflow(t *testing.T) {
	items := []map[string]any{}
	for i := 0; i < 160; i++ {
		items = append(items, map[string]any{"event": fmt.Sprintf("event:%03d", i), "label": strings.Repeat("<", 160)})
	}
	cursor := ""
	seen := 0
	for range 30 {
		result := summaryPage(items, map[string]any{"type": "events", "archived": true, "cursor": cursor, "limit": float64(50)}, "event")
		if result.IsError || len(encoded(result.StructuredContent)) > 31<<10 {
			t.Fatal("event history unreadable")
		}
		seen += len(result.StructuredContent["data"].([]map[string]any))
		cursor, _ = result.StructuredContent["nextCursor"].(string)
		if cursor == "" {
			break
		}
	}
	if seen != len(items) {
		t.Fatal("history page lost items", seen)
	}
	first := summaryPage(items, map[string]any{"type": "events", "archived": true, "limit": float64(1)}, "event")
	if !summaryPage(items, map[string]any{"type": "events", "archived": false, "cursor": first.StructuredContent["nextCursor"]}, "event").IsError {
		t.Fatal("cursor survived filter change")
	}
}

func TestMutationOutputOverflowKeepsTypedOriginalReceipt(t *testing.T) {
	receipt := api.Approval{ID: "original-approval", Status: "sent", Description: strings.Repeat("original permission", 10000)}
	out := compactValue(map[string]any{"ok": true, "outcome": "sent", "data": receipt}, false)
	if !out.IsError || out.StructuredContent["data"].(map[string]any)["id"] != receipt.ID || out.StructuredContent["outcome"] != "sent" {
		t.Fatal("overflow erased effect identity")
	}
}

func TestRegisteredEventAndWorkerMessageReadsUseBoundedOriginalJSON(t *testing.T) {
	r, _, _ := fixture(t)
	configureNativeSources(t, r)
	r.ConfigureEvents(&eventFixtureTools{}, t.TempDir())
	args := map[string]any{"request": map[string]any{"type": "tool", "id": "large-original", "label": "Original", "server": "fixture/mail", "tool": "read", "arguments": map[string]any{"original": strings.Repeat("原文<&>", 800)}, "outputSchema": map[string]any{"type": "object"}}}
	registered := r.CallTool(t.Context(), "bot_event", encoded(args))
	if registered.IsError {
		t.Fatal(registered.StructuredContent)
	}
	original, err := r.events.Event("large-original")
	if err != nil {
		t.Fatal(err)
	}
	got := collectBoundedJSON(t, func(cursor string) api.ToolResult {
		return r.CallTool(t.Context(), "bot_event", encoded(map[string]any{"request": map[string]any{"type": "read", "event": "event:large-original", "cursor": cursor}}))
	})
	if got != string(encoded(original)) {
		t.Fatal("event read changed source")
	}
	f := &largeMessageTasks{message: strings.Repeat("原文<&>", 10000)}
	r.ConfigureTasks(f, nil)
	got = collectBoundedJSON(t, func(cursor string) api.ToolResult {
		return r.CallTool(t.Context(), "bot_tasks", encoded(map[string]any{"request": map[string]any{"type": "read", "task": "owned", "messageId": "original", "cursor": cursor}}))
	})
	expected := map[string]any{"status": "completed", "message": api.TaskConversationMessage{ID: "original", Role: "assistant", Text: f.message}}
	if got != string(encoded(expected)) {
		t.Fatal("Worker original message changed")
	}
}

type largeMessageTasks struct {
	compactTasks
	message string
}

func (f *largeMessageTasks) ReadTaskConversation(_ context.Context, _ string) (api.TaskConversation, error) {
	return api.TaskConversation{Status: "completed", Messages: []api.TaskConversationMessage{{ID: "original", Role: "assistant", Text: f.message}}}, nil
}

type failureReadTasks struct{ largeMessageTasks }

func (f *failureReadTasks) ReadTaskConversation(ctx context.Context, id string) (api.TaskConversation, error) {
	v, err := f.largeMessageTasks.ReadTaskConversation(ctx, id)
	v.Status = "failed"
	v.Failure = api.UnknownTaskFailure("codex", "original-turn")
	return v, err
}
func (f *failureReadTasks) ObserveTask(context.Context, string) (api.WorkObservation, error) {
	return api.WorkObservation{State: api.WorkState{Task: api.Task{ID: "owned", Status: "failed", Failure: api.UnknownTaskFailure("codex", "original-turn")}}, Items: []api.Item{{Text: f.message}}}, nil
}
func TestWorkerFailureVisibleBesideChunkedMessagesAndObservation(t *testing.T) {
	r, _, _ := fixture(t)
	f := &failureReadTasks{largeMessageTasks: largeMessageTasks{message: strings.Repeat("prior progress ", 10000)}}
	r.ConfigureTasks(f, nil)
	for _, request := range []map[string]any{{"type": "read", "task": "owned", "messageId": "original"}, {"type": "read", "task": "owned"}, {"type": "observe", "task": "owned"}} {
		out := r.CallTool(t.Context(), "bot_tasks", encoded(map[string]any{"request": request}))
		if out.IsError || out.StructuredContent["failure"] == nil {
			t.Fatal("bounded output hid failure", out)
		}
		failure := out.StructuredContent["failure"].(*api.TaskFailure)
		if failure.Execution != "original-turn" || failure.Retryable != nil {
			t.Fatal("original metadata changed")
		}
	}
}
