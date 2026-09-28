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

func TestScheduledReviewNoticesSurviveQuietProjection(t *testing.T) {
	for _, status := range []string{"denied", "timedOut", "aborted", "failed", "inProgress", "approved"} {
		for _, dream := range []bool{false, true} {
			t.Run(status+map[bool]string{false: "/care", true: "/dream"}[dream], func(t *testing.T) {
				v := api.Snapshot{Connection: "ready", Phase: "completed", CurrentTurn: "automatic",
					Reviews: []api.Review{{ID: "native-review", Status: status}},
					Items:   []api.Item{{ID: "recap", TurnKey: "automatic", Kind: "assistant", Text: api.SilentReminder}}}
				turns := map[string]string{"automatic": "completed"}
				out := Present(v, turns, false)
				if dream {
					out = Dream(out, turns, false)
				}
				visible := status != "inProgress" && status != "approved"
				if out.Quiet == visible || (len(out.Reviews) == 1) != visible {
					t.Fatalf("review notice visibility differs from quiet status: %+v", out)
				}
				if len(out.Approvals) != 0 {
					t.Fatal("review created manual approval authority")
				}
			})
		}
	}
}
