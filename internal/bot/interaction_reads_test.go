package bot

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type retainedInteractionFixture struct {
	workerInteractionFixture
	approvals []api.Approval
	questions []api.WorkerQuestion
}

func (f *retainedInteractionFixture) WorkerApprovals() []api.Approval       { return f.approvals }
func (f *retainedInteractionFixture) WorkerQuestions() []api.WorkerQuestion { return f.questions }
func TestInteractionReadsRemainBoundedWithoutDiscardingOriginals(t *testing.T) {
	r, _, _ := fixture(t)
	f := &retainedInteractionFixture{}
	for n := 0; n < 200; n++ {
		f.approvals = append(f.approvals, api.Approval{ID: fmt.Sprint("resolved-", n), Status: "resolved", Description: strings.Repeat("old permission", 30)})
		f.questions = append(f.questions, api.WorkerQuestion{ID: fmt.Sprint("expired-", n), State: "expired", Title: strings.Repeat("old question", 30)})
	}
	for n := 0; n < 53; n++ {
		f.approvals = append(f.approvals, api.Approval{ID: fmt.Sprintf("pending-%02d", n), Status: "pending", Owner: "task", Description: strings.Repeat("original permission", 120), Choices: []api.Choice{{ID: "accept-session", Scope: "session"}}})
	}
	f.approvals = append(f.approvals, api.Approval{ID: "unknown-original", Status: "unknown", Owner: "task"}, api.Approval{ID: "large-original", Status: "pending", Owner: "task", Description: strings.Repeat("原始权限<&>\n", 4000), Choices: []api.Choice{{ID: "once", Scope: "once"}}})
	r.ConfigureWorkerInteractions(f)
	cursor := ""
	seen := map[string]bool{}
	for n := 0; n < 100; n++ {
		raw, _ := json.Marshal(map[string]any{"request": map[string]any{"type": "list", "limit": 4, "cursor": cursor}})
		result := r.CallTool(t.Context(), "bot_interactions", raw)
		if result.IsError || len(encoded(result.StructuredContent)) > 31<<10 {
			t.Fatal("bounded list lost pending requests", result.StructuredContent)
		}
		data := result.StructuredContent["data"].(map[string]any)
		for _, a := range data["approvals"].([]api.Approval) {
			if a.Status == "resolved" || seen[a.ID] {
				t.Fatal("terminal or duplicate list entry")
			}
			seen[a.ID] = true
		}
		for _, ref := range data["reads"].([]map[string]string) {
			seen[ref["id"]] = true
		}
		if len(data["questions"].([]api.WorkerQuestion)) != 0 {
			t.Fatal("expired questions consumed pending list")
		}
		cursor, _ = data["nextCursor"].(string)
		if cursor == "" {
			break
		}
	}
	if len(seen) != 54 || seen["unknown-original"] || !seen["large-original"] {
		t.Fatal("lost original pending or unknown", len(seen))
	}
	// All original fields survive JSON paging, including Unicode and exact offers.
	cursor = ""
	var reconstructed strings.Builder
	for n := 0; n < 100; n++ {
		raw, _ := json.Marshal(map[string]any{"request": map[string]any{"type": "read", "kind": "approval", "id": "large-original", "cursor": cursor}})
		result := r.CallTool(t.Context(), "bot_interactions", raw)
		if result.IsError {
			t.Fatal(result.StructuredContent)
		}
		data := result.StructuredContent["data"].(map[string]any)
		piece := data["json"].(string)
		if !utf8.ValidString(piece) {
			t.Fatal("broken Unicode chunk")
		}
		reconstructed.WriteString(piece)
		cursor, _ = data["nextCursor"].(string)
		if cursor == "" {
			if data["complete"] != true {
				t.Fatal("missing completion")
			}
			break
		}
	}
	original := f.approvals[len(f.approvals)-1]
	if reconstructed.String() != string(encoded(original)) {
		t.Fatal("approval original was truncated or rewritten")
	}
	closed := invokeCompact(t, r, "bot_interactions", `{"request":{"type":"read","kind":"approval","id":"resolved-0"}}`)
	if closed.StructuredContent["data"].(api.Approval).Description != f.approvals[0].Description {
		t.Fatal("closed original lost")
	}
	if f.decision.ID != "" {
		t.Fatal("reading changed native authorization")
	}
}
func TestInteractionCursorCannotMixChangedNativeRequest(t *testing.T) {
	f := &retainedInteractionFixture{approvals: []api.Approval{{ID: "original", Status: "pending", Description: strings.Repeat("x", 15000)}}}
	first := readInteraction(f, map[string]any{"id": "original", "kind": "approval"}).StructuredContent["data"].(map[string]any)
	f.approvals[0].Description += "changed"
	result := readInteraction(f, map[string]any{"id": "original", "kind": "approval", "cursor": first["nextCursor"]})
	if !result.IsError || result.StructuredContent["error"].(map[string]string)["code"] != "cursor_invalid" {
		t.Fatal("mixed native request revisions")
	}
}

func TestInteractionTodoExcludesReceiptReconciliation(t *testing.T) {
	for _, state := range []string{"", "pending", "sent", "sending", "dispatching", "unknown", "resolved", "answered", "expired", "dismissed", "superseded"} {
		f := &retainedInteractionFixture{approvals: []api.Approval{{ID: "original", Status: state}}, questions: []api.WorkerQuestion{{ID: "Q1", State: state}}}
		data := interactionPage(f, map[string]any{}).StructuredContent["data"].(map[string]any)
		if (len(data["approvals"].([]api.Approval)) == 1) != (state == "pending") || (len(data["questions"].([]api.WorkerQuestion)) == 1) != (state == "") {
			t.Fatalf("nonactionable receipt flooded todo: %s", state)
		}
		if readInteraction(f, map[string]any{"kind": "question", "id": "Q1"}).IsError {
			t.Fatal("receipt became unreadable")
		}
	}
}
