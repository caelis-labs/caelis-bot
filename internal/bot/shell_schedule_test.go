package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/testsupport/hostfixture"
)

type countedShell struct {
	ShellObserver
	calls atomic.Int32
}

func (s *countedShell) ObserveShell(ctx context.Context, command string) (json.RawMessage, error) {
	s.calls.Add(1)
	return s.ShellObserver.ObserveShell(ctx, command)
}

func scheduleFixture(t *testing.T, shell ShellObserver) (*Runtime, *fakeEngine, *time.Time) {
	t.Helper()
	r, f, now := fixture(t)
	t.Cleanup(r.Close)
	r.ConfigureShellObserver(shell)

	return r, f, now
}

func scheduleTick(t *testing.T, r *Runtime) {
	t.Helper()
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func observationSample(t *testing.T, r *Runtime) int {
	t.Helper()
	scheduleTick(t, r)
	ticks := 1
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		state := r.events.Snapshot()
		if len(state.Occurrences) == 0 || state.Occurrences[len(state.Occurrences)-1].Status != "collecting" {
			return ticks
		}
		select {
		case <-r.events.Changes():
			scheduleTick(t, r)
			ticks++
		case <-ctx.Done():
			t.Fatal("sample failed to settle")
		}
	}
}

func registerObservation(t *testing.T, r *Runtime, id, command, when string) {
	t.Helper()
	invokeCompact(t, r, "bot_schedule_update", string(encoded(map[string]any{"request": map[string]any{
		"type": "interval", "id": id, "context": "Report " + id, "seconds": 60, "observe": command, "when": when,
	}})))
}

type shellFunc func(context.Context, string) (json.RawMessage, error)

func (shellFunc) ShellObservationAvailable(context.Context) bool { return true }
func (f shellFunc) ObserveShell(ctx context.Context, command string) (json.RawMessage, error) {
	return f(ctx, command)
}

func TestClockOnlyDoesNotObserveAndUsesDistinctOccurrences(t *testing.T) {
	shell := &countedShell{}
	r, engine, now := scheduleFixture(t, shell)
	start := *now
	one := `{"request":{"type":"after","id":"one","seconds":30,"context":"Once"}}`
	invokeCompact(t, r, "bot_schedule_update", one)
	invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"interval","id":"periodic","seconds":60,"context":"Periodic"}}`)
	*now = start.Add(29 * time.Second)
	scheduleTick(t, r)
	if len(engine.submissions) != 0 || len(r.events.Snapshot().Occurrences) != 0 {
		t.Fatal("early clock wake")
	}
	for i, seconds := range []int{30, 60, 120} {
		*now = start.Add(time.Duration(seconds) * time.Second)
		for range 4 {
			scheduleTick(t, r)
		}
		if len(engine.submissions) != i+1 {
			t.Fatalf("at %ds: got %d wakes", seconds, len(engine.submissions))
		}
	}
	if shell.calls.Load() != 0 || engine.submissions[1].ID == engine.submissions[2].ID {
		t.Fatal("clock used Shell or reused a periodic occurrence")
	}
	t.Log("13 ticks, 0 Shell executions, 3 distinct wakes (1 one-off and 2 periodic)")
}

func TestScheduleObservationUnavailableDoesNotSave(t *testing.T) {
	r, _, _ := scheduleFixture(t, nil)
	result := invokeCompact(t, r, "bot_schedule", `{"request":{"type":"context"}}`)
	body := string(encoded(result.StructuredContent))
	if !strings.Contains(body, `"shellObservationAvailable":false`) || strings.Contains(body, "eventTools") || strings.Contains(body, "clock.minute") {
		t.Fatal(body)
	}
	out := r.CallTool(t.Context(), "bot_schedule_update", []byte(`{"request":{"type":"after","id":"no-core","seconds":1,"context":"Report result","observe":"echo {}","when":"true"}}`))
	if !out.IsError || !strings.Contains(string(encoded(out.StructuredContent)), "shell_observation_unavailable") ||
		len(r.events.Snapshot().Plans) != 0 || len(r.events.Snapshot().Events) != 0 {
		t.Fatal(out)
	}
	for name, raw := range map[string]string{
		"bot_event":           `{"request":{"type":"script","id":"old","label":"Old","interpreter":"/bin/sh","script":"echo {}","outputSchema":{"type":"object"}}}`,
		"bot_schedule_update": `{"request":{"type":"save","id":"old","label":"Old","prompt":"Wake","trigger":{"type":"event","sources":["clock.minute"],"condition":"true","timeZone":"UTC"}}}`,
	} {
		if !r.CallTool(t.Context(), name, []byte(raw)).IsError {
			t.Fatal("retired registration was accepted", name)
		}
	}
}

func TestScheduleObservationLateResultAndUnknownAreIsolated(t *testing.T) {
	for _, cancelPlan := range []bool{true, false} {
		t.Run(fmt.Sprintf("cancel=%t", cancelPlan), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			shell := &countedShell{ShellObserver: shellFunc(func(ctx context.Context, _ string) (json.RawMessage, error) {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				if cancelPlan {
					return json.RawMessage(`{"ready":true}`), nil
				}
				return nil, context.DeadlineExceeded
			})}
			r, engine, now := scheduleFixture(t, shell)
			registerObservation(t, r, "blocked", "fixture", "event.ready == true")
			invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"after","id":"other","seconds":61,"context":"Independent alarm"}}`)
			*now = now.Add(time.Minute)
			scheduleTick(t, r)
			ticks := 1
			select {
			case <-started:
			case <-t.Context().Done():
				t.Fatal("sample did not start")
			}
			*now = now.Add(time.Second)
			for range 3 {
				scheduleTick(t, r)
				ticks++
			}
			if len(engine.submissions) != 1 || !strings.Contains(engine.submissions[0].Text, "Independent alarm") {
				t.Fatal("alarm waited for observation")
			}
			if cancelPlan {
				invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"remove","automation":"schedule:blocked"}}`)
			}
			close(release)
			deadline := time.NewTimer(10 * time.Second)
			defer deadline.Stop()
			for {
				state := r.events.Snapshot()
				if state.Occurrences[0].Status != "collecting" {
					want := "unknown"
					if cancelPlan {
						want = "cancelled"
					}
					if state.Occurrences[0].Status != want {
						t.Fatal(state.Occurrences)
					}
					break
				}
				select {
				case <-r.events.Changes():
					scheduleTick(t, r)
					ticks++
				case <-deadline.C:
					t.Fatal("blocked sample did not settle")
				}
			}
			*now = now.Add(10 * time.Minute)
			for range 4 {
				scheduleTick(t, r)
				ticks++
			}
			if len(engine.submissions) != 1 || shell.calls.Load() != 1 {
				t.Fatal("late or unconfirmed observation woke or replayed")
			}
			t.Logf("%d ticks, 1 observation, 1 independent alarm, 0 observation wakes", ticks)
		})
	}
}

func platformObservation(posix, windows string) string {
	if runtime.GOOS == "windows" {
		return windows
	}
	return posix
}

func TestNotebookScheduleWithManagedCore(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_SHELL_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_SHELL_BINARY to a Shell-capable Core build")
	}
	store := hostfixture.New(t, bin, "store")
	store.IsolateRecoveryEnvironment(t)
	dir := filepath.Join(store.Root, "Notebook")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	process, err := store.StartForeground(ctx)
	if err != nil {
		t.Fatal(err)
	}
	native := caelis.NewNotebookObserver(api.RuntimeSettings{Runtime: "caelis", CaelisStore: store.Path}, dir)
	retry := time.NewTicker(20 * time.Millisecond)
	defer retry.Stop()
	for !native.ShellObservationAvailable(ctx) {
		select {
		case <-retry.C:
		case <-ctx.Done():
			t.Fatal("private Host did not advertise Shell", ctx.Err())
		case <-process.Done():
			t.Fatal("private Host exited before Shell readiness")
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var service atomic.Value
	service.Store(`{"phase":"pending"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, service.Load().(string))
	}))
	defer server.Close()
	httpCommand := platformObservation("curl -fsS "+server.URL, "(Invoke-WebRequest -UseBasicParsing '"+server.URL+"').Content")
	fileCommand := platformObservation(
		`if [ -f artifact.json ]; then cat artifact.json; else printf '{"phase":"absent"}'; fi`,
		`if (Test-Path artifact.json) { Get-Content -Raw artifact.json } else { '{"phase":"absent"}' }`)
	combined := platformObservation(
		`if [ -f manifest.txt ] && [ "$(curl -fsS `+server.URL+`)" = '{"phase":"ready"}' ]; then printf '{"ready":true,"summary":"manifest and service ready"}'; else printf '{"ready":false}'; fi`,
		`if ((Test-Path manifest.txt) -and (Invoke-RestMethod '`+server.URL+`').phase -eq 'ready') { '{"ready":true,"summary":"manifest and service ready"}' } else { '{"ready":false}' }`)

	for _, tc := range []struct {
		name, command, when, want string
		steps                     []func()
		restart                   bool
	}{
		{"file", fileCommand, `event.phase == "ready"`, "artifact-v2", []func(){
			func() {}, func() { write("artifact.json", `{"phase":"writing"}`) },
			func() { write("artifact.json", `{"phase":"ready","version":"artifact-v2"}`) },
		}, true},
		{"http-ready", httpCommand, `event.phase != "pending"`, "thumbnail-ready", []func(){
			func() { service.Store(`{"phase":"pending"}`) }, func() { service.Store(`{"phase":"ready","output":"thumbnail-ready"}`) },
		}, false},
		{"http-failure", httpCommand, `event.phase != "pending"`, "unsupported-format", []func(){
			func() { service.Store(`{"phase":"pending"}`) }, func() { service.Store(`{"phase":"failed","reason":"unsupported-format"}`) },
		}, false},
		{"ci-success", httpCommand, `event.status == "completed"`, "success", []func(){
			func() { service.Store(`{"status":"in_progress","conclusion":null}`) },
			func() { service.Store(`{"status":"completed","conclusion":"success"}`) },
		}, false},
		{"ci-failure", httpCommand, `event.status == "completed"`, "failure", []func(){
			func() { service.Store(`{"status":"in_progress","conclusion":null}`) },
			func() { service.Store(`{"status":"completed","conclusion":"failure"}`) },
		}, false},
		{"batch-version", platformObservation("cat batch.json", "Get-Content -Raw batch.json"), `event.version > 7`, "index-v8", []func(){
			func() { write("batch.json", `{"version":7}`) }, func() { write("batch.json", `{"version":8,"index":"index-v8"}`) },
		}, false},
		{"two-sources", combined, "event.ready == true", "manifest and service ready", []func(){
			func() { service.Store(`{"phase":"pending"}`) }, func() { write("manifest.txt", "artifact-v2") },
			func() { service.Store(`{"phase":"ready"}`) },
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shell := &countedShell{ShellObserver: native}
			r, engine, now := scheduleFixture(t, shell)
			*now = time.Now().UTC().Truncate(time.Second)
			registerObservation(t, r, tc.name, tc.command, tc.when)
			scheduleTick(t, r)
			ticks := 1
			if shell.calls.Load() != 0 {
				t.Fatal("registration or early tick executed observe")
			}
			for i, prepare := range tc.steps {
				prepare()
				*now = now.Add(time.Minute)
				ticks += observationSample(t, r)
				for range 3 {
					scheduleTick(t, r)
					ticks++
				}
				if want := i == len(tc.steps)-1; (len(engine.submissions) == 1) != want {
					t.Fatalf("wrong wake count at sample %d: %d", i+1, len(engine.submissions))
				}
				if i == 0 && tc.restart {
					old := r
					old.Close()
					var err error
					r, err = New(old.path, nil)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(r.Close)
					r.now, r.engine = old.now, engine
					r.ConfigureShellObserver(shell)
					registerObservation(t, r, tc.name, tc.command, tc.when)
				}
			}
			*now = now.Add(2 * time.Hour)
			for range 3 {
				scheduleTick(t, r)
				ticks++
			}
			if shell.calls.Load() != int32(len(tc.steps)) || len(engine.submissions) != 1 ||
				!strings.Contains(engine.submissions[0].Text, tc.want) || !strings.Contains(engine.submissions[0].Text, "untrusted data") {
				t.Fatalf("samples=%d, wakes=%+v", shell.calls.Load(), engine.submissions)
			}
			t.Logf("%d ticks, %d real Shell executions, 1 wake, payload contains %q", ticks, shell.calls.Load(), tc.want)
		})
	}

	for _, fault := range []struct{ name, command, issue string }{
		{"nonzero", platformObservation(`printf '{"phase":"failed"}'; exit 7`, `'{"phase":"failed"}'; exit 7`), "collection_failed"},
		{"malformed", platformObservation("printf 'not-json'", "'not-json'"), "output_contract"},
	} {
		t.Run(fault.name, func(t *testing.T) {
			shell := &countedShell{ShellObserver: native}
			r, engine, now := scheduleFixture(t, shell)
			registerObservation(t, r, "fault", fault.command, `event.phase == "failed"`)
			invokeCompact(t, r, "bot_schedule_update", `{"request":{"type":"after","id":"alarm","seconds":61,"context":"Independent alarm"}}`)
			ticks := 0
			for i := range 3 {
				*now = now.Add(time.Minute)
				ticks += observationSample(t, r)
				if i == 0 {
					*now = now.Add(time.Second)
				}
				for range 3 {
					scheduleTick(t, r)
					ticks++
				}
			}
			if len(engine.submissions) != 1 || !strings.Contains(engine.submissions[0].Text, "Independent alarm") || shell.calls.Load() != 3 {
				t.Fatalf("observation fault affected the other arrangement: %v/%d", engine.submissions, shell.calls.Load())
			}
			state := r.events.Snapshot()
			if state.Plans[0].Issue != fault.issue || !state.Plans[0].Finished.IsZero() || len(state.Occurrences) != 1 {
				t.Fatal("fault consumed the arrangement or produced a business result", state)
			}
			t.Logf("%d ticks, 3 real Shell samples, 0 observation wakes, 1 independent alarm", ticks)
		})
	}
}

func TestScheduleToolRetainsDefinitionAndReusesItAfterRestart(t *testing.T) {
	observer := &countedShell{ShellObserver: shellFunc(func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"revision":9}`), nil
	})}
	r, engine, now := scheduleFixture(t, observer)
	first := `{"request":{"type":"after","id":"retained","seconds":1,"observe":"check revision","when":"event.revision > 8","retain":true}}`
	invokeCompact(t, r, "bot_schedule_update", first)
	*now = now.Add(time.Second)
	observationSample(t, r)
	expectActive := func(r *Runtime, want bool) {
		t.Helper()
		out := invokeCompact(t, r, "bot_schedule", `{"request":{"type":"list","kind":"registered"}}`)
		n := out.StructuredContent["data"].(map[string]any)["total"]
		if (n != 0) != want {
			t.Fatalf("active schedule count %v, want nonzero=%t", n, want)
		}
	}
	expectActive(r, false)
	r.Close()
	restarted, err := New(r.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restarted.now, restarted.engine = r.now, engine
	restarted.ConfigureShellObserver(observer)
	invokeCompact(t, restarted, "bot_schedule_update", first)
	expectActive(restarted, false)
	invokeCompact(t, restarted, "bot_schedule_update", `{"request":{"type":"after","id":"reuse","seconds":1,"event":"event:retained"}}`)
	expectActive(restarted, true)
	*now = now.Add(time.Second)
	observationSample(t, restarted)
	expectActive(restarted, false)
	invokeCompact(t, restarted, "bot_schedule_update", `{"request":{"type":"remove","automation":"event:retained"}}`)
	if len(engine.submissions) != 2 || engine.submissions[0].ID == engine.submissions[1].ID || observer.calls.Load() != 2 ||
		len(restarted.events.Snapshot().Events) != 0 || len(restarted.deliveries.state.Records) != 0 {
		t.Fatal("reuse, dedup or receipt storage did not settle", engine.submissions)
	}
}
