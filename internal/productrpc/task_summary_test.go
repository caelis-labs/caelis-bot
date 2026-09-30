package productrpc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type summaryProduct struct {
	fakeProduct
	summaries []api.TaskSummary
}

func (p *summaryProduct) TaskSummaries() []api.TaskSummary { return p.summaries }
func TestTaskSummaryPreservesUnknownTerminalFactAndOpaqueID(t *testing.T) {
	p := &summaryProduct{summaries: []api.TaskSummary{{ID: "native-task-binding", Title: "Export", Status: "completed", Outcome: "unknown", Pinned: true}}}
	projection := projection{key: []byte("private-fixture")}
	state, err := projection.state(Scope{BotID: "bot", Generation: "generation"}, Cursor{Revision: "1"}, p)
	if err != nil || len(state.TaskSummaries) != 1 {
		t.Fatal(state, err)
	}
	task := state.TaskSummaries[0]
	if task.ID == p.summaries[0].ID || task.Status != "completed" || task.Outcome != "unknown" || !task.Pinned {
		t.Fatal("receipt uncertainty changed native terminal fact", task)
	}
	if p.summaries[0].ID != "native-task-binding" {
		t.Fatal("projection mutated owner")
	}
	encoded, _ := json.Marshal(state)
	if strings.Contains(string(encoded), "native-task-binding") || strings.Contains(string(encoded), "workspace") {
		t.Fatal("native task binding leaked")
	}
	p.summaries = make([]api.TaskSummary, MaxTaskSummaries+1)
	if _, err = projection.state(Scope{}, Cursor{}, p); err == nil {
		t.Fatal("unbounded watchlist admitted")
	}
	p.summaries = []api.TaskSummary{{ID: "task", Title: strings.Repeat("x", 4097)}}
	if _, err = projection.state(Scope{}, Cursor{}, p); err == nil {
		t.Fatal("unbounded task title admitted")
	}
}
