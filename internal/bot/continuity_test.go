package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/continuity"
)

type reviewFixture struct {
	*systemDeliveryFixture
	observation continuity.Observation
	current     api.ConversationState
}

func (f *reviewFixture) ContinuityObservation() continuity.Observation { return f.observation }
func (f *reviewFixture) ConversationState() api.ConversationState      { return f.current }

func reviewTestRuntime(t *testing.T, provider string) (*Runtime, *reviewFixture, *time.Time) {
	t.Helper()
	r, err := NewForRuntime(filepath.Join(t.TempDir(), "bot.json"), provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	f := &reviewFixture{systemDeliveryFixture: &systemDeliveryFixture{ready: true, outcome: "accepted", receipts: map[string]api.Receipt{}}, current: api.ConversationState{Session: "resident", Turn: "turn-64", Status: "completed", Observed: true, Idle: true}}
	f.observation = continuity.Observation{Session: "resident", Turn: "turn-64", Since: now.Add(-14 * 24 * time.Hour), CompletedAt: now, Turns: 64, Compactions: 3}
	r.engine = f
	return r, f, &now
}
func reviewTicks(t *testing.T, r *Runtime) {
	t.Helper()
	for range 2 {
		if err := r.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
func nextReviewTurn(f *reviewFixture, now time.Time) {
	f.observation.Turns++
	f.observation.Turn = fmt.Sprintf("turn-%d", f.observation.Turns)
	f.observation.CompletedAt = now
	f.current.Turn = f.observation.Turn
}

func TestContinuityInvitationUsesSystemReceiptAndAllowsDeferral(t *testing.T) {
	for _, provider := range []string{"codex", "caelis"} {
		t.Run(provider, func(t *testing.T) {
			r, f, now := reviewTestRuntime(t, provider)
			reviewTicks(t, r)
			if len(f.inputs) != 1 || !f.inputs[0].HostNotice || f.inputs[0].Dream || !f.inputs[0].Scheduled {
				t.Fatal("wrong independent input route", f.inputs)
			}
			text := f.inputs[0].Text
			for _, required := range []string{"continuity_review", "unavailable", "Current user instructions", "Keep working", "references/memory.md", "separate decisions", api.SilentReminder} {
				if !strings.Contains(text, required) {
					t.Fatal("missing invitation boundary", required)
				}
			}
			original := f.inputs[0].ID
			if r.SystemReceipt(original).Outcome != "accepted" {
				t.Fatal("missing original receipt")
			}
			// Bot chooses to keep working; there is no handoff or renewal call.
			*now = now.Add(continuity.Cooldown - time.Hour)
			f.observation.Compactions += 3
			for range 64 {
				nextReviewTurn(f, *now)
				reviewTicks(t, r)
			}
			if len(f.inputs) != 1 || f.current.Session != "resident" {
				t.Fatal("deferral/cooldown replaced the session or repeated invitation")
			}
			*now = now.Add(time.Hour)
			reviewTicks(t, r)
			if len(f.inputs) != 1 {
				t.Fatal("idle elapsed time created a model turn")
			}
			nextReviewTurn(f, *now)
			reviewTicks(t, r)
			if len(f.inputs) != 2 || f.inputs[1].ID == original {
				t.Fatal("new accumulated evidence after cooldown missing")
			}
		})
	}
}

func TestContinuityUnknownRestartPreservesOriginalReceipt(t *testing.T) {
	r, f, now := reviewTestRuntime(t, "codex")
	f.outcome = "unknown"
	reviewTicks(t, r)
	original := *r.review.state.Pending
	if len(f.inputs) != 1 {
		t.Fatal(f.inputs)
	}
	reopened, err := NewForRuntime(r.path, "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	reopened.engine, reopened.now = f, func() time.Time { return *now }
	*now = now.Add(30 * 24 * time.Hour)
	f.observation.Session, f.current.Session = "changed-session", "changed-session"
	nextReviewTurn(f, *now)
	for range 3 {
		reviewTicks(t, reopened)
	}
	if len(f.inputs) != 1 || !reflect.DeepEqual(*reopened.review.state.Pending, original) {
		t.Fatal("unknown was replayed or reidentified")
	}
	f.receipts[original.ID] = api.Receipt{ID: original.ID, Outcome: "accepted"}
	reviewTicks(t, reopened)
	if reopened.review.state.Pending != nil || reopened.review.state.LastID != original.ID || len(f.inputs) != 1 {
		t.Fatal("original terminal proof not reconciled")
	}
}

func TestContinuityHumanPriorityAndDeferredAdmission(t *testing.T) {
	r, f, _ := reviewTestRuntime(t, "caelis")
	r.userWaiting.Store(1)
	reviewTicks(t, r)
	original := r.review.state.Pending.ID
	if len(f.inputs) != 0 {
		t.Fatal("notice overtook queued human")
	}
	r.userWaiting.Store(0)
	f.deferred, f.outcome = true, "rejected"
	reviewTicks(t, r)
	if r.review.state.Pending.ID != original || !r.review.state.NotBefore.IsZero() {
		t.Fatal("deferred notice consumed cooldown")
	}
	f.deferred, f.outcome = false, "accepted"
	reviewTicks(t, r)
	for _, input := range f.inputs {
		if input.ID != original {
			t.Fatal("deferred attempt got another ID")
		}
	}
	if r.review.state.Pending != nil {
		t.Fatal("deferred message did not settle")
	}
}

func TestContinuityDoesNotWakeFromAgeHistoryFailureOrRollback(t *testing.T) {
	for _, scenario := range []string{"healthy", "stale", "failed", "busy", "unobserved", "rollback", "no-new-turn"} {
		t.Run(scenario, func(t *testing.T) {
			r, f, now := reviewTestRuntime(t, "codex")
			switch scenario {
			case "healthy":
				f.observation.Compactions = 0
			case "stale":
				f.observation.CompletedAt = now.Add(-time.Hour)
			case "failed":
				f.current.Status = "failed"
			case "busy":
				f.current.Idle = false
			case "unobserved":
				f.current.Observed = false
			case "rollback":
				r.review.state.LastNow = now.Add(time.Hour)
			case "no-new-turn":
				r.review.state.Evaluated, r.review.state.Session = f.observation.Turns, f.observation.Session
			}
			reviewTicks(t, r)
			if len(f.inputs) != 0 {
				t.Fatal("ineligible observation woke model", scenario)
			}
		})
	}
}

func TestContinuityCorruptStateAndWriteFailureDoNotDispatch(t *testing.T) {
	r, f, _ := reviewTestRuntime(t, "codex")
	r.review.path = t.TempDir() // cannot atomically replace a directory
	if err := r.Tick(t.Context()); err == nil || len(f.inputs) != 0 || r.review.state.Pending != nil {
		t.Fatal("failed preparation dispatched or consumed evidence", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(r.path), "continuity-codex.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(r.path, nil)
	if err != nil {
		t.Fatal("local feature fault blocked chat", err)
	}
	reopened.engine = f
	reviewTicks(t, reopened)
	if len(f.inputs) != 0 {
		t.Fatal("corruption permitted notice")
	}
	if receipt, err := reopened.SubmitUser(t.Context(), api.Submission{ID: "user", Text: "Continue"}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
}

func TestContinuityDefinitiveRejectionConsumesCooldownAndCredits(t *testing.T) {
	r, f, now := reviewTestRuntime(t, "codex")
	f.outcome = "rejected"
	reviewTicks(t, r)
	if len(f.inputs) != 1 || r.review.state.Pending != nil || r.review.state.NotBefore != now.Add(continuity.Cooldown) {
		t.Fatal("definitive rejection did not settle and cool down")
	}
	original := f.inputs[0].ID
	if r.SystemReceipt(original).Outcome != "rejected" {
		t.Fatal("original rejection lost")
	}
	*now = now.Add(continuity.Cooldown)
	nextReviewTurn(f, *now)
	f.outcome = "accepted"
	reviewTicks(t, r)
	if len(f.inputs) != 1 {
		t.Fatal("cooldown expiry reused consumed evidence")
	}
	f.observation.Turns += 62
	f.observation.Compactions += 3
	nextReviewTurn(f, *now)
	reviewTicks(t, r)
	if len(f.inputs) != 2 || f.inputs[1].ID == original {
		t.Fatal("fresh evidence did not earn a fresh invitation")
	}
}
