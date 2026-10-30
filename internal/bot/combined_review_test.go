package bot

import "testing"

// Combination-only regression: an invitation prepared for the old completed
// head cannot become the first input of a newly committed handoff.
func TestCombinedPreparedReviewRespectsHandoffAdmission(t *testing.T) {
	for _, provider := range []string{"codex", "caelis"} {
		t.Run(provider, func(t *testing.T) {
			r, engine, _ := reviewTestRuntime(t, provider)
			if err := r.ReviewContinuity(t.Context()); err != nil || r.review.state.Pending == nil {
				t.Fatal("invitation not prepared", err)
			}
			if err := r.openHandoff(); err != nil {
				t.Fatal(err)
			}
			if err := r.handoff.saveTurn("dream-original", "resident", "turn-64", "Original task and authorization; review path and unread scope."); err != nil {
				t.Fatal(err)
			}
			if err := r.ReviewContinuity(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(engine.inputs) != 0 || r.review.state.Pending == nil || r.handoff.pending().Superseded {
				t.Fatal("prepared review bypassed or invalidated pending handoff")
			}
			if err := r.handoff.record("dream-original", "next", "committed"); err != nil {
				t.Fatal(err)
			}
			engine.current.Session, engine.observation.Session = "next", "next"
			if err := r.ReviewContinuity(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(engine.inputs) != 0 || r.review.state.Pending != nil || r.PrepareHandoffContext().HandoffDigest == "" {
				t.Fatal("old invitation dispatched into new binding or consumed handoff")
			}
		})
	}
}
