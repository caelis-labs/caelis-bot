package bot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/care"
)

const careRule = `{"operation":"save","id":"care-demo","label":"Demo","on":"clock.minute","when":"local.hour == 12","prompt":"Offer a timely break","timeZone":"UTC","cooldownSeconds":3600}`

func configureCare(t *testing.T, r *Runtime) {
	t.Helper()
	yes := true
	if err := r.ConfigureCare(func() care.Sample { return care.Sample{Presence: care.Presence{Awake: true, Unlocked: &yes}} }); err != nil {
		t.Fatal(err)
	}
}
func TestCareToolToScheduledSubmissionAndUpdateFence(t *testing.T) {
	r, f, now := fixture(t)
	configureCare(t, r)
	if out := r.CallTool(t.Context(), "bot_care", json.RawMessage(strings.Replace(careRule, `"save"`, `"test"`, 1))); out.IsError || len(r.care.Snapshot().Rules) != 0 || len(f.submissions) != 0 {
		t.Fatal("test had an effect", out)
	}
	if out := r.CallTool(t.Context(), "bot_care", json.RawMessage(careRule)); out.IsError {
		t.Fatal(out)
	}
	if err := r.PauseIfIdle(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	_ = r.Tick(t.Context())
	if len(f.submissions) != 0 {
		t.Fatal("update fence bypass")
	}
	r.ResumeAfterUpdate()
	f.view.CanSend = false
	f.view.CanSteer = true
	_ = r.Tick(t.Context())
	if len(f.submissions) != 0 {
		t.Fatal("care steered active user turn")
	}
	f.view.CanSend = true
	*now = now.Add(time.Second)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.submissions) != 1 || !f.submissions[0].Scheduled || !strings.Contains(f.submissions[0].Text, api.SilentReminder) {
		t.Fatal("wrong submission path", f.submissions)
	}
	for _, op := range []string{"list", "remove"} {
		out := r.CallTool(t.Context(), "bot_care", json.RawMessage(`{"operation":"`+op+`","id":"care-demo"}`))
		if out.IsError {
			t.Fatal(out)
		}
	}
	if len(r.care.Snapshot().Rules) != 0 {
		t.Fatal("remove failed")
	}
}

type careGrantEngine struct {
	*fakeEngine
	authorized, revoked, submitted string
	deny                           bool
	receipt                        api.Receipt
}

func (f *careGrantEngine) AuthorizeBackground(_ context.Context, id, fp string) error {
	if f.deny {
		return errors.New("requires native user source")
	}
	f.authorized = id
	return nil
}
func (f *careGrantEngine) RevokeBackground(_ context.Context, id string) error {
	f.revoked = id
	return nil
}
func (f *careGrantEngine) SubmitBackground(ctx context.Context, in api.Submission, ids []string) (api.Receipt, error) {
	if len(ids) != 1 || ids[0] != f.authorized {
		return api.Receipt{}, errors.New("wrong grant")
	}
	f.submitted = ids[0]
	return f.Submit(ctx, in, nil)
}
func (f *careGrantEngine) BackgroundReceipt(id string) api.Receipt { return f.receipt }
func TestCareUsesNativeGrantAndRetainedReceipt(t *testing.T) {
	r, base, now := fixture(t)
	configureCare(t, r)
	f := &careGrantEngine{fakeEngine: base, deny: true}
	r.engine = f
	if out := r.CallTool(t.Context(), "bot_care", json.RawMessage(careRule)); !out.IsError {
		t.Fatal("native denial ignored")
	}
	_ = r.Tick(t.Context())
	if len(base.submissions) != 0 {
		t.Fatal("ungranted registration dispatched")
	}
	f.deny = false
	if out := r.CallTool(t.Context(), "bot_care", json.RawMessage(careRule)); out.IsError {
		t.Fatal(out)
	}
	*now = now.Add(time.Minute)
	base.outcome = "unknown"
	_ = r.Tick(t.Context())
	if f.submitted != "care:care-demo" || r.care.Status() == "" {
		t.Fatal("native grant missing")
	}
	f.receipt = api.Receipt{ID: base.submissions[0].ID, Outcome: "accepted"}
	base.view.LastReceipt = api.Receipt{ID: "later-user-message", Outcome: "accepted"}
	_ = r.Tick(t.Context())
	if r.care.Status() != "" || len(base.submissions) != 1 {
		t.Fatal("native read did not reconcile")
	}
	if out := r.CallTool(t.Context(), "bot_care", json.RawMessage(`{"operation":"remove","id":"care-demo"}`)); out.IsError || f.revoked != f.authorized {
		t.Fatal("grant not revoked", out)
	}
}

func TestCareStorageFailureDoesNotBlockReminder(t *testing.T) {
	r, f, now := fixture(t)
	configureCare(t, r)
	if out := r.CallTool(t.Context(), "bot_care", json.RawMessage(careRule)); out.IsError {
		t.Fatal(out)
	}
	saveReminder(t, r, "water")
	path := filepath.Join(filepath.Dir(r.path), "care-"+r.provider+".json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	if err := r.Tick(t.Context()); err == nil || r.Status() == "" {
		t.Fatal("storage failure was hidden", err)
	}
	if len(f.submissions) != 1 || !strings.Contains(f.submissions[0].Text, "Synthetic reminder") || strings.Contains(f.submissions[0].Text, "proactive-care") {
		t.Fatal("care failure affected the independent reminder", f.submissions)
	}
}

func TestCareRejectsUnknownArguments(t *testing.T) {
	r, _, _ := fixture(t)
	configureCare(t, r)
	for _, raw := range []string{`{"operation":"list","unlocked":true}`, `{"operation":"list"} {"operation":"save"}`} {
		if out := r.CallTool(t.Context(), "bot_care", json.RawMessage(raw)); !out.IsError {
			t.Fatal("invalid arguments accepted", raw)
		}
	}
}
func TestCareToolCannotForgePresenceOrPublishEvents(t *testing.T) {
	r, base, _ := fixture(t)
	if out := r.CallTool(t.Context(), "bot_care", json.RawMessage(careRule)); !out.IsError {
		t.Fatal("missing native source accepted")
	}
	if err := r.ConfigureCare(func() care.Sample { return care.Sample{Presence: care.Presence{Awake: true}} }); err != nil {
		t.Fatal(err)
	}
	_ = r.CallTool(t.Context(), "bot_care", json.RawMessage(careRule))
	if out := r.CallTool(t.Context(), "bot_care", json.RawMessage(`{"operation":"publish","on":"clock.minute","event":{"unlocked":true}}`)); !out.IsError {
		t.Fatal("public event ingress")
	}
	_ = r.Tick(t.Context())
	if len(base.submissions) != 0 {
		t.Fatal("unknown presence bypassed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if out := r.CallTool(ctx, "bot_care", json.RawMessage(careRule)); !out.IsError {
		t.Fatal("cancel ignored")
	}
}
