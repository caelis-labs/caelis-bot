package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/eventplan"
)

const notebookInitializeFixture = `{
	"protocol_version":1, "api_version":"v1", "envelope_version":"caelis.control.envelope/v1",
	"store_id":"fixture-store", "instance_id":"fixture-host",
	"capabilities":["session-profile-control-v1","turn-steering-receipts-v1","session-profiles-v1",
		"session-hot-configuration-v1","session-native-execution-v1","session-workspace-binding-v1",
		"session-background-activation-v1","session-resource-transfer-v1","execution-configuration-v1",
		"session-guardian-review-v1","runtime-agent-team-v1","shell-execution-v1"]
}`

func notebookControlFixture(t *testing.T, handler http.HandlerFunc) *caelis.NotebookObserver {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	service := filepath.Join(dir, "runtime", "service")
	if err := os.MkdirAll(service, 0700); err != nil {
		t.Fatal(err)
	}
	discovery := fmt.Sprintf(`{"schema_version":"caelis.control.service-discovery/v1","endpoint":%q,"instance_id":"fixture-host","principal_id":"fixture-owner"}`, server.URL)
	if err := os.WriteFile(filepath.Join(service, "discovery.json"), []byte(discovery), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service, "auth.token"), []byte("fixture-only-token"), 0600); err != nil {
		t.Fatal(err)
	}
	notebook := filepath.Join(dir, "Notebook")
	if err := os.Mkdir(notebook, 0700); err != nil {
		t.Fatal(err)
	}
	return caelis.NewNotebookObserver(api.RuntimeSettings{Runtime: "caelis", CaelisStore: dir}, notebook)
}

func waitScheduleBarrier(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("schedule barrier did not complete")
	}
}

func TestRemoveCancelsNotebookControlInFlight(t *testing.T) {
	for _, stage := range []string{"initialize", "execute"} {
		t.Run(stage, func(t *testing.T) {
			entered, interrupted, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			var initializations, posts atomic.Int32
			waitForCancellation := func(req *http.Request) {
				close(entered)
				select {
				case <-req.Context().Done():
					close(interrupted)
				case <-release:
				}
			}
			observer := notebookControlFixture(t, func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/api/control/v1/initialize":
					if initializations.Add(1) == 2 && stage == "initialize" {
						waitForCancellation(req)
					}
					fmt.Fprint(w, notebookInitializeFixture)
				case "/api/control/v1/shell/execute":
					if _, err := io.Copy(io.Discard, req.Body); err != nil {
						t.Error(err)
						return
					}
					if posts.Add(1) == 1 && stage == "execute" {
						waitForCancellation(req)
					}
					fmt.Fprint(w, `{"location":"sandbox","exit_code":0,"stdout":"{}"}`)
				default:
					http.NotFound(w, req)
				}
			})
			r, engine, now := scheduleFixture(t, observer)
			registerObservation(t, r, "sample", "printf '{}'", "true")
			*now = now.Add(time.Minute)
			scheduleTick(t, r)
			waitScheduleBarrier(t, entered)
			invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"remove","automation":"schedule:sample"}}`)
			raw, err := os.ReadFile(filepath.Join(filepath.Dir(r.path), "events-"+r.provider+".json"))
			var saved eventplan.State
			if err != nil || json.Unmarshal(raw, &saved) != nil || len(saved.Plans) != 1 || saved.Plans[0].Status != "cancelled" {
				t.Fatal("cancellation was not committed", err)
			}
			waitScheduleBarrier(t, interrupted)
			observationSample(t, r)
			*now = now.Add(time.Hour)
			for range 3 {
				scheduleTick(t, r)
			}
			wantPosts := int32(0)
			if stage == "execute" {
				wantPosts = 1
				s := r.events.Snapshot()
				if len(saved.Occurrences) != 1 || len(s.Occurrences) != 1 || s.Occurrences[0].Status != "unknown" || s.Occurrences[0].ID != saved.Occurrences[0].ID {
					t.Fatalf("already admitted POST lost its uncertain original occurrence: %+v", s)
				}
			}
			if posts.Load() != wantPosts || initializations.Load() != 2 || len(engine.submissions) != 0 {
				t.Fatalf("cancelled sample escaped: initialize=%d posts=%d wakes=%d", initializations.Load(), posts.Load(), len(engine.submissions))
			}
		})
	}
}

func TestClockWakesBeforeBlockedNotebookCapabilityProbeReturns(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	observer := notebookControlFixture(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/control/v1/initialize" {
			t.Error("registration attempted a Shell execution")
			http.NotFound(w, req)
			return
		}
		close(entered)
		select {
		case <-release:
		case <-req.Context().Done():
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	r, engine, now := scheduleFixture(t, observer)
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"after","id":"alarm","seconds":1}}`)
	*now = now.Add(time.Second)
	result := make(chan api.ToolResult, 1)
	go func() {
		result <- r.CallTool(t.Context(), "bot_schedule_update", json.RawMessage(`{"request":{"type":"interval","id":"watch","seconds":60,"observe":"printf '{}'","when":"true"}}`))
	}()
	waitScheduleBarrier(t, entered)
	ticked := make(chan error, 1)
	go func() { ticked <- r.Tick(t.Context()) }()
	select {
	case err := <-ticked:
		if err != nil || len(engine.submissions) != 1 {
			t.Fatal("independent clock failed during probe", err, engine.submissions)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler could not dispatch before the capability probe returned")
	}
	once.Do(func() { close(release) })
	select {
	case out := <-result:
		if !out.IsError || len(r.events.Snapshot().Plans) != 1 {
			t.Fatal("failed capability probe registered an observation", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("capability probe did not exit")
	}
}

func TestClockDeadlineRecheckedAfterSystemDeliveryIntent(t *testing.T) {
	r, engine, now := scheduleFixture(t, nil)
	deadline := now.Add(time.Second)
	reads := 0
	r.now = func() time.Time {
		reads++
		if reads > 1 {
			return deadline
		}
		return *now
	}
	msg := api.SystemMessage{ID: "deadline-fixture", Source: api.SystemCallback, Text: "Clock wake", Grants: []string{"schedule:clock"}}
	receipt, err := r.submitSystemBeforeLocked(t.Context(), msg, deadline)
	if err != nil || receipt.ID != msg.ID || receipt.Outcome != "rejected" || !receipt.Deferred ||
		len(engine.submissions) != 0 || len(r.deliveries.state.Records) != 0 {
		t.Fatal("expired native submission left a delivery effect or fence", receipt, err)
	}
}
