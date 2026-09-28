package activation

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestDreamKeepsOnlyRecapAndPreservesApproval(t *testing.T) {
	v := api.Snapshot{Connection: "ready", Phase: "completed", CurrentTurn: "dream", Items: []api.Item{
		{ID: "user", TurnKey: "ordinary", Kind: "user", Text: "hello"},
		{ID: "progress", TurnKey: "dream", Kind: "assistant", Text: "reading memory"},
		{ID: "file", TurnKey: "dream", Kind: "activity", Text: "private handoff path"},
		{ID: "final", TurnKey: "dream", Kind: "assistant", Text: "Finished the work."},
	}}
	out := Dream(v, map[string]string{"dream": "completed"}, false)
	if len(out.Items) != 2 || out.Items[1].ID != "final" || !out.Quiet {
		t.Fatal(out)
	}
	v.Approvals = []api.Approval{{Status: "pending"}}
	if Dream(v, map[string]string{"dream": "running"}, false).Quiet {
		t.Fatal("approval hidden")
	}
}
