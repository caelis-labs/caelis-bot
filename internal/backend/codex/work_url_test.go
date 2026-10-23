package codex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWorkerURLFormOriginalReceiptAndIndependentNativeConsent(t *testing.T) {
	for _, loss := range []bool{false, true} {
		name := "confirmed"
		if loss {
			name = "lost-http-response"
		}
		t.Run(name, func(t *testing.T) {
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.Header().Set("Content-Type", "text/html")
					io.WriteString(w, `<form method="post"><input type="hidden" name="nonce" value="native-nonce"><input type="password" name="secret" required><button>Continue</button></form>`)
					return
				}
				posts.Add(1)
				if loss {
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
					return
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			s, f, _, m := taskPair(t)
			sendSynthetic(t, s, "url-form-parent")
			task := newTask(t, m, "url-form-worker")
			thread, run := workerNativeTarget(t, s, task.ID)
			f.emit(wireMessage{ID: raw("native-url-form"), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{"threadId": thread, "turnId": run, "mode": "url", "serverName": "temporary MCP", "url": server.URL, "message": "Complete original"})})
			awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
			a := s.Snapshot().Approvals[0]
			page, err := s.ReadWorkURL(t.Context(), task.ID, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			in := api.WorkURLSubmission{TaskID: task.ID, ApprovalID: a.ID, RequestID: "original-url-submit", Revision: page.Revision, Values: map[string]string{"secret": "synthetic-sensitive-value"}}
			op, err := s.SubmitWorkURL(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			expected := "accepted"
			if loss {
				expected = "unknown"
			}
			if op.Outcome != expected {
				t.Fatal(op)
			}
			// Original/unknown receipt never replays HTTP, even if caller invokes it again.
			if _, err = s.SubmitWorkURL(t.Context(), in); err != nil || posts.Load() != 1 {
				t.Fatal(err, posts.Load())
			}
			changed := in
			changed.Values = map[string]string{"secret": "changed"}
			if _, err = s.SubmitWorkURL(t.Context(), changed); err == nil {
				t.Fatal("same ID different body accepted")
			}
			second := in
			second.RequestID = "replacement-url-submit"
			if op, err = s.SubmitWorkURL(t.Context(), second); err != nil || op.Outcome != "rejected" || posts.Load() != 1 {
				t.Fatal("new ID replayed original request", op, err)
			}
			select {
			case answer := <-f.answers:
				t.Fatal("web submission decided native", answer)
			default:
			}
			if bytes, err := os.ReadFile(s.opts.StateFile); err != nil || strings.Contains(string(bytes), "synthetic-sensitive-value") || strings.Contains(string(bytes), "native-nonce") {
				t.Fatal("browser secrets persisted", err)
			}
			if err = s.DecideWork(t.Context(), api.WorkDecision{TaskID: task.ID, RequestID: "native-form-consent", Decision: api.Decision{ID: a.ID, Choice: "accept"}}); err != nil {
				t.Fatal(err)
			}
			answer := <-f.answers
			var result map[string]any
			json.Unmarshal(answer.Result, &result)
			if string(answer.ID) != `"native-url-form"` || result["action"] != "accept" {
				t.Fatal("wrong original native response", answer)
			}
			s.mu.Lock()
			if s.binding.LostOwnerTasks == nil {
				s.binding.LostOwnerTasks = map[string]bool{}
			}
			s.binding.LostOwnerTasks[task.ID] = true
			s.mu.Unlock()
			if _, err = s.ReadWorkURL(t.Context(), task.ID, a.ID); err == nil {
				t.Fatal("dead owner still opened page")
			}
			// Loss of owner/request does not relabel the previously uncertain effect.
			receipt, err := s.ReadWorkURLSubmission(context.Background(), task.ID, a.ID, in.RequestID)
			if err != nil || receipt.Outcome != expected || posts.Load() != 1 {
				t.Fatal(receipt, err)
			}
		})
	}
}
func TestWorkerURLDeclineAndTurnChangeScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<form method="post"><button>Continue</button></form>`)
	}))
	defer server.Close()
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "url-scope-parent")
	task := newTask(t, m, "url-scope-worker")
	thread, run := workerNativeTarget(t, s, task.ID)
	f.emit(wireMessage{ID: raw("native-url-scope"), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{"threadId": thread, "turnId": run, "mode": "url", "serverName": "temporary MCP", "url": server.URL, "message": "Original request"})})
	awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	a := s.Snapshot().Approvals[0]
	if _, err := s.ReadWorkURL(t.Context(), task.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.binding.Tasks[task.ID].Run = "later-turn"
	s.mu.Unlock()
	if _, err := s.ReadWorkURL(t.Context(), task.ID, a.ID); err != nil {
		t.Fatal("ordinary turn change expired pending request", err)
	}
	if _, err := s.ReadWorkURL(t.Context(), "other-task", a.ID); err == nil {
		t.Fatal("task scope escaped")
	}
	if err := s.DecideWork(t.Context(), api.WorkDecision{TaskID: task.ID, RequestID: "native-scope-decline", Decision: api.Decision{ID: a.ID, Choice: "decline"}}); err != nil {
		t.Fatal(err)
	}
	<-f.answers
	if _, err := s.ReadWorkURL(t.Context(), task.ID, a.ID); err == nil {
		t.Fatal("declined request still readable")
	}
}

func TestWorkerURLNativeConsentResolutionDoesNotCompleteOrRevokeWebChallenge(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<form method="post"><button>Continue</button></form>`)
			return
		}
		posts.Add(1)
		w.WriteHeader(204)
	}))
	defer server.Close()
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "url-resolution-parent")
	task := newTask(t, m, "url-resolution-worker")
	thread, run := workerNativeTarget(t, s, task.ID)
	f.emit(wireMessage{ID: raw("native-url-resolution"), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{"threadId": thread, "turnId": run, "mode": "url", "serverName": "temporary MCP", "url": server.URL, "message": "Original web challenge"})})
	awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	a := s.Snapshot().Approvals[0]
	page, err := s.ReadWorkURL(t.Context(), task.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DecideWork(t.Context(), api.WorkDecision{TaskID: task.ID, RequestID: "original-accept-before-web", Decision: api.Decision{ID: a.ID, Choice: "accept"}}); err != nil {
		t.Fatal(err)
	}
	<-f.answers
	f.emit(wireMessage{Method: "serverRequest/resolved", Params: raw(map[string]any{"threadId": thread, "requestId": "native-url-resolution"})})
	awaitState(t, s, func(v api.Snapshot) bool { return v.Approvals[0].Status == "resolved" })
	if posts.Load() != 0 {
		t.Fatal("native consent implied web completion")
	}
	if _, err = s.ReadWorkURL(t.Context(), task.ID, a.ID); err != nil {
		t.Fatal("consent resolution revoked original live web challenge", err)
	}
	op, err := s.SubmitWorkURL(t.Context(), api.WorkURLSubmission{TaskID: task.ID, ApprovalID: a.ID, RequestID: "original-web-after-consent", Revision: page.Revision, Values: map[string]string{}})
	if err != nil || op.Outcome != "accepted" || posts.Load() != 1 {
		t.Fatal(op, err, posts.Load())
	}
	finishTask(s, f, task.ID, "completed")
	awaitState(t, s, func(v api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.binding.Tasks[task.ID].View.Status == "completed"
	})
	if _, err = s.ReadWorkURL(t.Context(), task.ID, a.ID); err == nil {
		t.Fatal("terminal native facts did not revoke page")
	}
}
