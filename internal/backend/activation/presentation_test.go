package activation

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestQuietScheduledStreamingCompletionAndHumanSteering(t *testing.T) {
	v := api.Snapshot{Connection: "ready", Phase: "working", CurrentTurn: "wake", CanInterrupt: true, Items: []api.Item{{ID: "u", TurnKey: "wake", Kind: "activation", Text: "private instruction"}, {ID: "a", TurnKey: "wake", Kind: "assistant", Text: "checking"}}}
	turns := map[string]string{"wake": "running"}
	got := Present(v, turns, false)
	if !got.Quiet || len(got.Items) != 0 || !got.CanInterrupt {
		t.Fatal(got)
	}
	v.Items = append(v.Items, api.Item{ID: "final", TurnKey: "wake", Kind: "assistant", Text: api.SilentReminder})
	v.Phase = "completed"
	v.CanInterrupt = false
	turns["wake"] = "completed"
	got = Present(v, turns, false)
	if !got.Quiet || len(got.Items) != 0 {
		t.Fatal("skip leaked", got)
	}
	v.Items[2].Text = "Time for water"
	got = Present(v, turns, false)
	if got.Quiet || len(got.Items) != 2 {
		t.Fatal("result lost", got)
	}
	v.Items = append(v.Items, api.Item{ID: "human", TurnKey: "wake", Kind: "user", Text: api.SilentReminder})
	turns["wake"] = "running"
	v.Phase = "working"
	got = Present(v, turns, false)
	if got.Quiet || got.Scheduled || len(got.Items) != 3 {
		t.Fatal("human steering hidden", got)
	}
	if v.Items[0].Kind != "activation" {
		t.Fatal("native history mutated")
	}
}
func TestQuietDoesNotHideApprovalFailureOrUnknown(t *testing.T) {
	for _, phase := range []string{"failed", "unknown", "interrupted"} {
		v := Present(api.Snapshot{Connection: "ready", Phase: phase, CurrentTurn: "wake"}, map[string]string{"wake": phase}, false)
		if v.Quiet {
			t.Fatal(phase)
		}
	}
	v := Present(api.Snapshot{Connection: "ready", Phase: "working", CurrentTurn: "wake", Approvals: []api.Approval{{Status: "pending"}}}, map[string]string{"wake": "running"}, false)
	if v.Quiet || len(v.Approvals) != 1 {
		t.Fatal(v)
	}
}
