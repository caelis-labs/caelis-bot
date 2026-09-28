package bot

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type dreamEngine struct {
	fakeEngine
	conversation      api.ConversationState
	dreams            []api.Submission
	cancels, renewals int
	outcome           string
}

func (e *dreamEngine) ConversationState() api.ConversationState { return e.conversation }
func (e *dreamEngine) SubmitDream(_ context.Context, in api.Submission) (api.Receipt, error) {
	e.dreams = append(e.dreams, in)
	e.conversation.Turn, e.conversation.Status, e.conversation.Idle = "dream-turn", "running", false
	return api.Receipt{ID: in.ID, Outcome: e.outcome}, nil
}
func (e *dreamEngine) DreamResult(id string) (api.Receipt, api.ConversationState) {
	return api.Receipt{ID: id, Outcome: e.outcome}, e.conversation
}
func (e *dreamEngine) CancelDream(context.Context, string) error {
	e.cancels++
	e.conversation.Status, e.conversation.Idle = "interrupted", true
	return nil
}
func (e *dreamEngine) RenewConversation(context.Context, string, string) error {
	e.renewals++
	e.conversation.Session, e.conversation.Turn, e.conversation.Status = "new", "", ""
	return nil
}

func dreamFixture(t *testing.T) (*Runtime, *dreamEngine, *time.Time) {
	r, _, now := fixture(t)
	v, err := notebook.OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	if err = r.ConfigureDream(v, filepath.Join(t.TempDir(), "app-skills", "caelis-bot-memory", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	e := &dreamEngine{fakeEngine: fakeEngine{outcome: "accepted"}, outcome: "accepted", conversation: api.ConversationState{Session: "old", Turn: "user-turn", Status: "completed", Idle: true}}
	r.engine = e
	return r, e, now
}
func beginDream(t *testing.T, r *Runtime, e *dreamEngine, now *time.Time) {
	t.Helper()
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(14 * time.Minute)
	_ = r.Tick(t.Context())
	if len(e.dreams) != 0 {
		t.Fatal("early Dream")
	}
	*now = now.Add(time.Minute)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(e.dreams) != 1 {
		t.Fatal("missing Dream")
	}
}
func TestDreamWaitsForUserAndSurvivesRestart(t *testing.T) {
	r, e, now := dreamFixture(t)
	beginDream(t, r, e, now)
	id := e.dreams[0].ID
	os.WriteFile(filepath.Join(r.dream.vault.Path(), notebook.HandoffName), []byte(notebook.DreamMarker(id)+"\nFinished X; no pending work."), 0600)
	e.conversation.Status, e.conversation.Idle = "completed", true
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(24 * time.Hour)
	for range 3 {
		_ = r.Tick(t.Context())
	}
	if len(e.dreams) != 1 || e.renewals != 0 {
		t.Fatal("idle model loop or eager session creation")
	}
	if err := r.ConfigureDream(r.dream.vault, filepath.Join(t.TempDir(), "caelis-bot-memory", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	receipt, err := r.SubmitUser(t.Context(), api.Submission{ID: "new-user", Text: "new topic"}, nil)
	if err != nil || receipt.Outcome != "accepted" || e.renewals != 1 || len(e.submissions) != 1 {
		t.Fatal(receipt, err, e.renewals)
	}
}
func TestDreamInterruptedByUserAndMissingHandoffNeverRotates(t *testing.T) {
	for _, finished := range []bool{false, true} {
		t.Run(map[bool]string{false: "interrupt", true: "missing-handoff"}[finished], func(t *testing.T) {
			r, e, now := dreamFixture(t)
			beginDream(t, r, e, now)
			if finished {
				e.conversation.Status, e.conversation.Idle = "completed", true
			}
			receipt, err := r.SubmitUser(t.Context(), api.Submission{ID: "new-user", Text: "hello"}, nil)
			if err != nil || receipt.Outcome != "accepted" || e.renewals != 0 || (!finished && e.cancels != 1) {
				t.Fatal(receipt, err, e.renewals, e.cancels)
			}
		})
	}
}
func TestDreamUnknownNeverRepeats(t *testing.T) {
	r, e, now := dreamFixture(t)
	e.outcome = "unknown"
	beginDream(t, r, e, now)
	*now = now.Add(time.Hour)
	for range 3 {
		_ = r.Tick(t.Context())
	}
	if len(e.dreams) != 1 || e.renewals != 0 {
		t.Fatal("unknown request repeated")
	}
}

func TestBackgroundActivityInvalidatesCompletedHandoff(t *testing.T) {
	r, e, now := dreamFixture(t)
	beginDream(t, r, e, now)
	id := e.dreams[0].ID
	os.WriteFile(filepath.Join(r.dream.vault.Path(), notebook.HandoffName), []byte(notebook.DreamMarker(id)+"\nFinished old work."), 0600)
	e.conversation.Status, e.conversation.Idle = "completed", true
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	e.conversation.Turn = "background-report"
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.SubmitUser(t.Context(), api.Submission{ID: "user-after-report", Text: "continue"}, nil)
	if e.renewals != 0 || r.dream.state.Attempt.Ready {
		t.Fatal("used stale handoff after background report")
	}
}
