package activation

import (
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestBackgroundResultSharesQuietPresentation(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name, status, text                           string
		artifact, approval, human, complete, visible bool
	}{
		{name: "streaming", status: "running", text: "checking"},
		{name: "skip", status: "completed", text: api.SilentReminder, complete: true},
		{name: "answer", status: "completed", text: "water", complete: true, visible: true},
		{name: "skip with file", status: "completed", text: api.SilentReminder, artifact: true, complete: true, visible: true},
		{name: "approval", status: "running", approval: true, visible: true},
		{name: "failure", status: "failed", complete: true, visible: true},
		{name: "cancelled", status: "cancelled", complete: true},
		{name: "human takes over", status: "running", human: true, text: "new human interaction", complete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := []api.Item{{Kind: "activation", TurnKey: "turn"}, {Kind: "assistant", TurnKey: "turn", Text: tc.text}, {Kind: "assistant", TurnKey: "other", Text: "unrelated"}}
			if tc.artifact {
				items[1].Artifacts = []api.Artifact{{ID: "file"}}
			}
			if tc.human {
				items = append(items, api.Item{Kind: "user", TurnKey: "turn"})
			}
			got := Observe(api.BackgroundResult{}, "care-id", "turn", tc.status, items, tc.approval, now)
			if got.Complete != tc.complete || got.Visible != tc.visible || got.Visible != !got.ObservedAt.IsZero() {
				t.Fatal(got)
			}
		})
	}
	old := Observe(api.BackgroundResult{}, "care-id", "turn", "running", nil, true, now)
	got := Observe(old, "care-id", "turn", "completed", []api.Item{{Kind: "assistant", TurnKey: "turn", Text: api.SilentReminder}}, false, now.Add(time.Hour))
	if !got.Complete || !got.Visible || !got.ObservedAt.Equal(now) {
		t.Fatal("approval refunded", got)
	}
}
