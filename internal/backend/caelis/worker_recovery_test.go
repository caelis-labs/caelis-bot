package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLegacyRetiredWorkerReadsOriginalSessionAndRetainsUnknownOperation(t *testing.T) {
	for _, status := range []string{"completed", "interrupted", "unknown", "working"} {
		t.Run(status, func(t *testing.T) {
			var reads atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
					t.Errorf("unexpected effect %s %s", r.Method, r.URL.Path)
					return
				}
				reads.Add(1)
				writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("original-turn"), Status: &status, Active: pointer(status == "working")}})
			})
			var original worker
			if err := json.Unmarshal([]byte(`{"retired":true,"native":true,"binding":{"session_id":"work","principal_id":"owner"},"task":{"id":"task","status":"unavailable","outcome":"unknown"},"promptID":"original-request"}`), &original); err != nil {
				t.Fatal(err)
			}
			s.state.Workers["task"] = original
			s.state.Operations["original-request"] = journal{Outcome: "unknown", Path: "/sessions/work/prompt", Digest: "original-digest"}
			for range 2 {
				if got, err := s.ReadWork(t.Context(), "task"); err != nil || got.Status != "unknown" || got.Outcome != "unknown" {
					t.Fatal(got, err)
				}
			}
			if reads.Load() != 2 {
				t.Fatal("cached viability blocked native read")
			}
			loaded, err := loadBinding(s.path)
			if err != nil || loaded.Workers["task"].Binding.SessionId != "work" || loaded.Workers["task"].PromptID != "original-request" || loaded.Operations["original-request"].Outcome != "unknown" || loaded.Operations["original-request"].Digest != "original-digest" {
				t.Fatal("lost original identity/receipt", err)
			}
			raw, err := os.ReadFile(s.path)
			if err != nil || strings.Contains(string(raw), `"retired"`) {
				t.Fatal("persisted viability", err)
			}
			if _, err := s.SendWork(authorizedWorkerCall(t.Context(), s), api.TaskMessage{ID: "task", RequestID: "new-input-forbidden", Prompt: "synthetic"}); err == nil {
				t.Fatal("unknown original operation replayed")
			}
		})
	}
}

func TestWorkerReadNativeRejectionIsRetryableAndNeverRecreates(t *testing.T) {
	for _, code := range []int{401, 403, 404, 409, 500, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			nativeCode := map[int]string{401: "unauthenticated", 403: "permission_denied", 404: "not_found", 409: "failed_precondition", 500: "internal", 503: "unavailable"}[code]
			var failed atomic.Bool
			failed.Store(true)
			var requests atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/original/state" {
					t.Errorf("rejection caused effect %s %s", r.Method, r.URL.Path)
					return
				}
				requests.Add(1)
				if failed.Load() {
					w.WriteHeader(code)
					payload := map[string]string{"code": nativeCode, "error": "PRIVATE_NATIVE_DETAIL"}
					if code == 409 {
						payload["kind"] = "session_closed"
					}
					writeFixture(w, payload)
					return
				}
				writeFixture(w, wire.SessionState{SessionId: "original", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false)}})
			})
			s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "original", PrincipalId: "owner"}, Task: api.Task{ID: "task", Status: "unknown"}, PromptID: "original-request"}
			_, err := s.ReadWork(t.Context(), "task")
			var native *remoteError
			if !errors.As(err, &native) || native.Status != code || native.Code != nativeCode || strings.Contains(err.Error(), "PRIVATE_NATIVE_DETAIL") {
				t.Fatal("native rejection lost or leaked", err)
			}
			failed.Store(false)
			if got, err := s.ReadWork(t.Context(), "task"); err != nil || got.Status != "completed" {
				t.Fatal("permanent rejection inferred", got, err)
			}
			if requests.Load() != 2 || s.state.Workers["task"].Binding.SessionId != "original" {
				t.Fatal("original binding changed")
			}
		})
	}
}

func TestExhaustedWorkerObservationStopsUntilExplicitRead(t *testing.T) {
	var requests atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false)}})
	})
	s.retainedWorkers = true
	s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "unknown"}}
	for i := range 3 {
		s.observationBudget.Take(time.Now().Add(time.Duration(i) * time.Hour))
	}
	ctx, cancel := context.WithCancel(t.Context())
	s.wg.Add(1)
	go s.pollLoop(ctx)
	time.Sleep(30 * time.Millisecond)
	cancel()
	s.wg.Wait()
	if requests.Load() != 0 {
		t.Fatal("exhausted background observation still polls", requests.Load())
	}
	if got, err := s.ReadWork(t.Context(), "task"); err != nil || got.Status != "completed" || requests.Load() != 1 {
		t.Fatal("explicit original retry blocked", got, err)
	}
}

func TestWorkerActivityIsNotRestoredFromDisk(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") })
	s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "unknown"}, Activity: "idle"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Active: pointer(false)}}}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	loaded := New(Options{Directory: filepath.Dir(s.path)})
	loaded.connected = true
	if got := loaded.WorkStates()[0]; got.Activity != "" {
		t.Fatal("old connection idle released new capacity", got)
	}
}

func TestOldNativeReadCannotReleaseNewWorkerActivity(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("old-turn"), Status: pointer("completed"), Active: pointer(false)}})
	})
	s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "working"}, Activity: "active"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("current-turn"), Status: pointer("working"), Active: pointer(true)}}, RetiredTurns: map[string]bool{"old-turn": true}}
	if _, err := s.ReadWork(t.Context(), "task"); err == nil {
		t.Fatal("old turn adopted")
	}
	if got := s.WorkStates()[0]; got.Activity == "idle" || got.Task.Status != "working" {
		t.Fatal("old turn released current capacity", got)
	}
}
