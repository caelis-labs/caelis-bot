package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type fakeEngine struct {
	view        api.Snapshot
	outcome     string
	submissions []api.Submission
}

func (f *fakeEngine) Snapshot() api.Snapshot { return f.view }
func (f *fakeEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	f.submissions = append(f.submissions, in)
	r := api.Receipt{ID: in.ID, Outcome: f.outcome}
	f.view.LastReceipt = r
	return r, nil
}
func fixture(t *testing.T) (*Runtime, *fakeEngine, *time.Time) {
	t.Helper()
	r, e := New(filepath.Join(t.TempDir(), "bot.json"), nil)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	f := &fakeEngine{view: api.Snapshot{CanSend: true}, outcome: "accepted"}
	r.engine = f
	return r, f, &now
}
func saveReminder(t *testing.T, r *Runtime, id string) Schedule {
	t.Helper()
	s, e := r.Upsert(Schedule{ID: id, Label: id, Prompt: "Synthetic reminder", EveryMinutes: 1})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestSleepCoalescesAndBusyWorkIsNeverSteered(t *testing.T) {
	r, f, now := fixture(t)
	var notifications []string
	r.SetReminderNotifier(func(id, title string) { notifications = append(notifications, title); _ = r.State() })
	saveReminder(t, r, "water")
	saveReminder(t, r, "break")
	*now = now.Add(6 * time.Hour)
	f.view.CanSend = false
	f.view.CanSteer = true
	if e := r.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(f.submissions) != 0 || r.State().Wake.Status != "pending" {
		t.Fatal("busy work was changed")
	}
	if len(notifications) != 1 || notifications[0] != "water、break" {
		t.Fatal("busy reminder was lost or was not coalesced", notifications)
	}
	f.view.CanSend = true
	if e := r.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(f.submissions) != 1 || len(r.State().Wake.ScheduleIDs) != 2 {
		t.Fatal("missed occurrences must coalesce")
	}
	if e := r.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(f.submissions) != 1 {
		t.Fatal("replayed accepted occurrence")
	}
	if len(notifications) != 1 {
		t.Fatal("notified the same due occurrence again")
	}
}
func TestUnknownDispatchNeverReplaysAndReconcilesReceipt(t *testing.T) {
	r, f, now := fixture(t)
	saveReminder(t, r, "water")
	*now = now.Add(time.Minute)
	f.outcome = "unknown"
	_ = r.Tick(context.Background())
	id := r.State().Wake.ID
	*now = now.Add(24 * time.Hour)
	for range 3 {
		_ = r.Tick(context.Background())
	}
	if len(f.submissions) != 1 {
		t.Fatal("unknown request replayed")
	}
	f.view.LastReceipt = api.Receipt{ID: id, Outcome: "accepted"}
	_ = r.Tick(context.Background())
	if r.State().Wake.Status != "accepted" {
		t.Fatal("native receipt ignored")
	}
}
func TestPersistenceBeforeDispatchAndStableIdentity(t *testing.T) {
	r, f, now := fixture(t)
	saveReminder(t, r, "water")
	*now = now.Add(time.Minute)
	original := r.path
	r.path = t.TempDir()
	if r.Tick(context.Background()) == nil || len(f.submissions) != 0 {
		t.Fatal("executed without durable occurrence")
	}
	r.path = original
	before := r.State()
	restored, e := New(original, nil)
	if e != nil {
		t.Fatal(e)
	}
	if restored.State().ID != before.ID {
		t.Fatal("Bot identity changed")
	}
}
func TestIdenticalSaveDoesNotMoveReminderAndRemovePreservesOtherQueuedWork(t *testing.T) {
	r, f, now := fixture(t)
	s := saveReminder(t, r, "water")
	saveReminder(t, r, "break")
	*now = now.Add(30 * time.Second)
	same := saveReminder(t, r, "water")
	if !same.Next.Equal(s.Next) {
		t.Fatal("retry rescheduled reminder")
	}
	*now = now.Add(time.Minute)
	f.view.CanSend = false
	_ = r.Tick(context.Background())
	if e := r.Remove("water"); e != nil {
		t.Fatal(e)
	}
	w := r.State().Wake
	if w == nil || len(w.ScheduleIDs) != 1 || w.ScheduleIDs[0] != "break" || strings.Contains(w.Prompt, "water") {
		t.Fatal("removed unrelated queued reminder")
	}
}
func TestRestartSkipsQuitPeriodAndDoesNotResetUnknown(t *testing.T) {
	r, _, now := fixture(t)
	saveReminder(t, r, "water")
	r.mu.Lock()
	r.state.Schedules[0].Next = time.Now().Add(-6 * time.Hour)
	r.state.Wake = &Wake{ID: "wake-uncertain", Status: "dispatching"}
	if e := r.saveLocked(); e != nil {
		t.Fatal(e)
	}
	r.mu.Unlock()
	restored, e := New(r.path, nil)
	if e != nil {
		t.Fatal(e)
	}
	s := restored.State()
	if s.Wake.Status != "unknown" || !s.Schedules[0].Next.After(time.Now().Add(-time.Second)) {
		t.Fatal("quit gap replayed")
	}
	_ = now
}
func TestDailyScheduleUsesZoneAcrossDST(t *testing.T) {
	loc, e := time.LoadLocation("America/New_York")
	if e != nil {
		t.Fatal(e)
	}
	after := time.Date(2026, 3, 7, 18, 0, 0, 0, loc)
	next, e := nextTime(Schedule{Daily: "18:00", TimeZone: "America/New_York"}, after)
	if e != nil {
		t.Fatal(e)
	}
	if next.Hour() != 18 || next.Sub(after) != 23*time.Hour {
		t.Fatal("daily wall time drifted", next)
	}
}
func TestPrivateBridgeRejectsWrongTokenAndRoundTripsRealMCP(t *testing.T) {
	r, _, _ := fixture(t)
	b, e := Serve(r)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	endpoint := b.listener.Endpoint()
	if !forward(endpoint, toolRequest{Token: "wrong", Name: "bot_clock"}).IsError {
		t.Fatal("unauthenticated command accepted")
	}
	t.Setenv("CAELIS_BOT_ENDPOINT", endpoint)
	t.Setenv("CAELIS_BOT_TOKEN", b.token)
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"bot_reminders","arguments":{"operation":"save","id":"test","label":"Test","prompt":"synthetic","everyMinutes":5}}}
`
	var out bytes.Buffer
	if e = RunStdio(strings.NewReader(in), &out); e != nil {
		t.Fatal(e)
	}
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) != nil {
			t.Fatal("invalid MCP output")
		}
		lines = append(lines, v)
	}
	if len(lines) != 3 || len(r.State().Schedules) != 1 || lines[2]["result"].(map[string]any)["isError"] != false {
		t.Fatal("MCP invocation did not reach host")
	}
	info, e := os.Stat(endpoint)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("socket permissions")
	}
}
func TestGestureAllowlist(t *testing.T) {
	r, _, _ := fixture(t)
	seen := ""
	r.action = func(action string) error { seen = action; return nil }
	if r.Perform("approve") == nil || seen != "" {
		t.Fatal("gesture gained execution authority")
	}
	if e := r.Perform("nod"); e != nil || seen != "nod" {
		t.Fatal("valid action missing")
	}
}

func TestBotApprovalIsAnExplicitToolAllowlist(t *testing.T) {
	r, _, _ := fixture(t)
	b, err := Serve(r)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c := b.Config("synthetic")
	if len(c.ApprovedTools) != 9 {
		t.Fatal("approval scope grew without review")
	}
	policy := map[string]bool{}
	for _, name := range c.ApprovedTools {
		policy[name] = true
	}
	for _, name := range []string{"bot_clock", "bot_reminders", "bot_gesture", "bot_tasks", "bot_task_start", "bot_task_read", "bot_task_send", "bot_task_stop", "bot_memory"} {
		if !policy[name] {
			t.Fatal("owned tool approval missing", name)
		}
	}

	for _, spec := range toolSpecs() {
		tool := spec.(map[string]any)
		if tool["description"] == "" || !policy[tool["name"].(string)] {
			t.Fatal("catalog/policy mismatch")
		}
	}
}

func TestIdentitySharedButSchedulesCannotCrossRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.json")
	first, e := NewForRuntime(path, "codex", nil)
	if e != nil {
		t.Fatal(e)
	}
	saved := saveReminder(t, first, "codex-only")
	second, e := NewForRuntime(path, "caelis", nil)
	if e != nil {
		t.Fatal(e)
	}
	if first.State().ID != second.State().ID {
		t.Fatal("runtime switch replaced Bot identity")
	}
	if _, e = second.Upsert(Schedule{ID: "codex-only", Label: "changed", Prompt: "changed", EveryMinutes: 1}); e == nil {
		t.Fatal("other runtime replaced schedule")
	}
	if e = second.Remove("codex-only"); e == nil {
		t.Fatal("other runtime removed schedule")
	}
	engine := &fakeEngine{view: api.Snapshot{CanSend: true}, outcome: "accepted"}
	second.engine = engine
	second.now = func() time.Time { return saved.Next.Add(time.Minute) }
	if e = second.Tick(t.Context()); e != nil {
		t.Fatal(e)
	}
	if len(engine.submissions) != 0 || !second.State().Schedules[0].Next.Equal(saved.Next) {
		t.Fatal("reminder ran on wrong runtime")
	}
}

func TestQueuedWakeRetainsRuntimeAcrossRestart(t *testing.T) {
	r, f, now := fixture(t)
	saveReminder(t, r, "queued")
	*now = now.Add(time.Minute)
	f.view.CanSend = false
	if e := r.Tick(t.Context()); e != nil {
		t.Fatal(e)
	}
	wake := *r.State().Wake
	other, e := NewForRuntime(r.path, "caelis", nil)
	if e != nil {
		t.Fatal(e)
	}
	engine := &fakeEngine{view: api.Snapshot{CanSend: true}, outcome: "accepted"}
	other.engine = engine
	if e = other.Tick(t.Context()); e != nil {
		t.Fatal(e)
	}
	if len(engine.submissions) != 0 || other.State().Wake.ID != wake.ID || !strings.Contains(other.Status(), "所属运行时") {
		t.Fatal("queued activation crossed runtime")
	}
	resumed, e := NewForRuntime(r.path, "codex", nil)
	if e != nil {
		t.Fatal(e)
	}
	resumed.engine = engine
	if e = resumed.Tick(t.Context()); e != nil {
		t.Fatal(e)
	}
	if len(engine.submissions) != 1 || engine.submissions[0].ID != wake.ID {
		t.Fatal("original wake not resumed")
	}
}

func TestApplicationToolHandlerHonorsCancellationAndShutdown(t *testing.T) {
	r, _, _ := fixture(t)
	defs := r.Definitions()
	if len(defs) != 9 {
		t.Fatal("incomplete application catalog")
	}
	defs[0].Name = "foreign"
	if r.Definitions()[0].Name == "foreign" {
		t.Fatal("catalog mutated")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if out := r.CallTool(ctx, "bot_reminders", json.RawMessage(`{"operation":"save","id":"no-write","label":"no","prompt":"no","everyMinutes":1}`)); !out.IsError || len(r.State().Schedules) != 0 {
		t.Fatal("cancelled tool mutated state")
	}
	if out := r.CallTool(t.Context(), "bot_clock", json.RawMessage(`{}`)); out.IsError {
		t.Fatal(out)
	}
	r.Stop()
	if out := r.CallTool(t.Context(), "bot_clock", json.RawMessage(`{}`)); !out.IsError {
		t.Fatal("stopped host still callable")
	}
}

type grantEngine struct {
	*fakeEngine
	background [][]string
}

func (f *grantEngine) AuthorizeBackground(context.Context, string, string) error { return nil }
func (f *grantEngine) RevokeBackground(context.Context, string) error            { return nil }
func (f *grantEngine) SubmitBackground(_ context.Context, in api.Submission, ids []string) (api.Receipt, error) {
	f.background = append(f.background, append([]string(nil), ids...))
	r := api.Receipt{ID: in.ID, Outcome: "accepted"}
	f.view.LastReceipt = r
	return r, nil
}
func TestGrantedRemindersDispatchIndividuallyWithoutUserSubmit(t *testing.T) {
	r, f, now := fixture(t)
	g := &grantEngine{fakeEngine: f}
	r.engine = g
	saveReminder(t, r, "water")
	saveReminder(t, r, "break")
	*now = now.Add(time.Minute)
	for range 3 {
		if e := r.Tick(t.Context()); e != nil {
			t.Fatal(e)
		}
	}
	if len(f.submissions) != 0 || len(g.background) != 2 || len(g.background[0]) != 1 || len(g.background[1]) != 1 || g.background[0][0] == g.background[1][0] {
		t.Fatal("grant wakes must be separate, exactly once, and never user messages", g.background, f.submissions)
	}
}
