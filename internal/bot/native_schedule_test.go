package bot

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
	"os"
	"strings"
	"testing"
	"time"
)

func configureNativeSources(t *testing.T, r *Runtime) {
	t.Helper()
	yes := true
	if err := r.ConfigureNativeSources(func() scheduledata.Sample {
		return scheduledata.Sample{Presence: scheduledata.Presence{Awake: true, Unlocked: &yes}}
	}); err != nil {
		t.Fatal(err)
	}
}

type scheduleGrantEngine struct {
	*fakeEngine
	authorized, revoked, submitted string
	deny                           bool
	receipt                        api.Receipt
}

func (f *scheduleGrantEngine) AuthorizeBackground(_ context.Context, id, fp string) error {
	if f.deny {
		return errors.New("requires native user source")
	}
	f.authorized = id
	return nil
}
func (f *scheduleGrantEngine) RevokeBackground(_ context.Context, id string) error {
	f.revoked = id
	return nil
}
func (f *scheduleGrantEngine) SubmitSystem(ctx context.Context, message api.SystemMessage) (api.Receipt, error) {
	in, ids := message.NativeInput(), message.Grants
	if len(ids) != 1 || ids[0] != f.authorized {
		return api.Receipt{}, errors.New("wrong grant")
	}
	f.submitted = ids[0]
	return f.Submit(ctx, in, nil)
}
func (f *scheduleGrantEngine) SystemReceipt(id string) api.Receipt { return f.receipt }

const nativeRequest = `{"request":{"type":"save","id":"demo","label":"Native","prompt":"Offer a timely break","trigger":{"type":"event","sources":["clock.minute"],"condition":"true","timeZone":"UTC"}}}`

type retainedReminderEngine struct {
	*fakeEngine
	receipt api.Receipt
	reads   []string
}

func (f *retainedReminderEngine) SystemReceipt(id string) api.Receipt {
	f.reads = append(f.reads, id)
	return f.receipt
}

func TestReminderRecoveryPrecedesNativeEvent(t *testing.T) {
	for _, retained := range []bool{false, true} {
		name := "legacy-last-receipt"
		if retained {
			name = "retained-receipt-after-later-input"
		}
		t.Run(name, func(t *testing.T) {
			r, base, now := fixture(t)
			saveReminder(t, r, "water")
			*now = now.Add(time.Minute)
			base.outcome = "unknown"
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			id := r.State().Wake.ID
			receipt := api.Receipt{ID: id, Outcome: "accepted"}
			base.view.LastReceipt = receipt
			if retained {
				r.engine = &retainedReminderEngine{fakeEngine: base, receipt: receipt}
				base.view.LastReceipt = api.Receipt{ID: "later-user-message", Outcome: "accepted"}
			}
			base.outcome = "accepted"
			configureNativeSources(t, r)
			if out := r.CallTool(t.Context(), "bot_schedule_update", json.RawMessage(nativeRequest)); out.IsError {
				t.Fatal(out)
			}
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if w := r.State().Wake; w.ID != id || w.Status != "accepted" {
				t.Fatal("reminder receipt lost to care dispatch", w)
			}
			var persisted State
			b, err := os.ReadFile(r.path)
			if err != nil || json.Unmarshal(b, &persisted) != nil || persisted.Wake.Status != "accepted" {
				t.Fatal("recovery was not persisted", err)
			}
			if len(base.submissions) != 1 {
				t.Fatal("new work dispatched before reminder reconciliation", base.submissions)
			}
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(base.submissions) != 2 || !strings.Contains(base.submissions[1].Text, "[Bot system event: callback;") {
				t.Fatal("native event did not resume after reconciliation", base.submissions)
			}
			*now = now.Add(time.Minute)
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(base.submissions) != 3 || !strings.Contains(base.submissions[2].Text, "Synthetic reminder") || base.submissions[2].ID == id {
				t.Fatal("later reminder blocked or old reminder replayed", base.submissions)
			}
		})
	}
}

func TestReminderRecoveryRequiresExactDurableAcceptance(t *testing.T) {
	for _, outcome := range []string{"unknown", "rejected", "wrong-id", "write-failure"} {
		t.Run(outcome, func(t *testing.T) {
			r, base, now := fixture(t)
			saveReminder(t, r, "water")
			*now = now.Add(time.Minute)
			base.outcome = "unknown"
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			id := r.State().Wake.ID
			f := &retainedReminderEngine{fakeEngine: base, receipt: api.Receipt{ID: id, Outcome: outcome}}
			r.engine = f
			base.view.LastReceipt = api.Receipt{ID: id, Outcome: "accepted"} // Exact provider evidence takes priority.
			configureNativeSources(t, r)
			if out := r.CallTool(t.Context(), "bot_schedule_update", json.RawMessage(nativeRequest)); out.IsError {
				t.Fatal(out)
			}
			switch outcome {
			case "wrong-id":
				f.receipt = api.Receipt{ID: "other-wake", Outcome: "accepted"}
			case "write-failure":
				f.receipt.Outcome = "accepted"
				r.path = t.TempDir()
			}
			for range 2 {
				err := r.Tick(t.Context())
				if (err != nil) != (outcome == "write-failure") {
					t.Fatal("wrong persistence error", err)
				}
				if w := r.State().Wake; w.ID != id || w.Status != "unknown" {
					t.Fatal("uncertain wake released", w)
				}
				if len(base.submissions) != 1 {
					t.Fatal("unresolved wake allowed a new dispatch", base.submissions)
				}
			}
			wantReads := 2
			if outcome == "rejected" || outcome == "write-failure" {
				wantReads = 1
			} // Terminal delivery proof is persisted independently of Wake.
			if len(f.reads) != wantReads || f.reads[0] != id || f.reads[len(f.reads)-1] != id {
				t.Fatal("receipt not read by exact wake ID", f.reads)
			}
		})
	}
}
