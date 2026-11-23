package bot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type idleConversationFixture struct {
	legacyDreamEngine
	calls   int
	failure error
}

func (e *idleConversationFixture) RenewIdleConversation(_ context.Context, _ string, head api.ConversationState) error {
	e.calls++
	if head != e.state {
		return errors.New("native head changed")
	}
	if e.failure != nil {
		return e.failure
	}
	e.state.Session = "fresh"
	return nil
}

func TestIdleConversationUses48HoursAndOnlyRealUserInput(t *testing.T) {
	for _, scenario := range []struct {
		name                           string
		age                            time.Duration
		busy, fail, duplicate, control bool
		want                           int
	}{
		{name: "24 hours", age: 24 * time.Hour},
		{name: "before threshold", age: 48*time.Hour - time.Nanosecond},
		{name: "exact threshold", age: 48 * time.Hour, want: 1},
		{name: "active turn or pending decision", age: 49 * time.Hour, busy: true},
		{name: "failed creation", age: 49 * time.Hour, fail: true, want: 1},
		{name: "duplicate original request", age: 49 * time.Hour, duplicate: true},
		{name: "hidden control reply", age: 49 * time.Hour, control: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := t.Context()
			r, _, now := fixture(t)
			engine := &idleConversationFixture{legacyDreamEngine: legacyDreamEngine{
				fakeEngine: fakeEngine{view: api.Snapshot{CanSend: !scenario.busy}, outcome: "accepted"},
				state:      api.ConversationState{Session: "original", Turn: "original-turn", Observed: true},
			}}
			if scenario.fail {
				engine.failure = errors.New("fixture native create failed")
			}
			r.engine = engine
			previous := *now
			r.ConfigureIdleConversation(func(context.Context, string) (time.Time, bool, error) { return previous, scenario.duplicate, nil })
			*now = now.Add(scenario.age)
			if err := r.Tick(ctx); err != nil || engine.calls != 0 {
				t.Fatal("timer created a session", err)
			}
			if !scenario.busy {
				notice := api.SystemMessage{ID: "worker-completion", Text: "latest worker result", Source: api.SystemWorkerResult}
				if receipt, err := r.SubmitSystem(ctx, notice); err != nil || receipt.Outcome != "accepted" {
					t.Fatal(receipt, err)
				}
				if !r.idle.last.IsZero() {
					t.Fatal("Worker result refreshed human activity")
				}
			}
			input := api.Submission{ID: "next-user", Text: "return to work"}
			if scenario.control {
				input.ModelInputOverride = "hidden native control envelope"
			}
			if receipt, err := r.SubmitUser(ctx, input, nil); err != nil || receipt.Outcome != "accepted" {
				t.Fatal(receipt, err)
			}
			if engine.calls != scenario.want || (engine.state.Session == "fresh") != (scenario.want > 0 && !scenario.fail) {
				t.Fatal("unexpected renewal or binding", engine.calls, engine.state.Session)
			}
		})
	}
}

func TestContinuousUserConversationsNeverAgeOut(t *testing.T) {
	r, _, now := fixture(t)
	engine := &idleConversationFixture{legacyDreamEngine: legacyDreamEngine{
		fakeEngine: fakeEngine{view: api.Snapshot{CanSend: true}, outcome: "accepted"},
		state:      api.ConversationState{Session: "original", Turn: "original-turn", Observed: true},
	}}
	r.engine = engine
	previous := *now
	r.ConfigureIdleConversation(func(context.Context, string) (time.Time, bool, error) { return previous, false, nil })
	for _, id := range []string{"day-1", "day-2", "day-3", "day-4"} {
		*now = now.Add(24 * time.Hour)
		if out, err := r.SubmitUser(t.Context(), api.Submission{ID: id, Text: "daily work"}, nil); err != nil || out.Outcome != "accepted" {
			t.Fatal(out, err)
		}
	}
	if engine.calls != 0 {
		t.Fatal("regular user activity was ignored")
	}
}
