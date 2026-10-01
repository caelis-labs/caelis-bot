package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWorkerJournalSyncBarrierPrecedesNativeDispatchAndAllowsSafeRetry(t *testing.T) {
	for _, stage := range []string{"before-publication", "directory-sync"} {
		t.Run(stage, func(t *testing.T) {
			d := workerPair(t)
			in := d.intent(t, 1)
			s := d.w.engine
			failure := errors.New("fixture journal unavailable")
			entered, release := make(chan struct{}), make(chan struct{})
			writes := 0
			s.mu.Lock()
			s.writeBinding = func(path string, data []byte) error {
				writes++
				if writes != 1 {
					return writeBindingFile(path, data, (*os.File).Sync)
				}
				barrier := func() error {
					close(entered)
					<-release
					return failure
				}
				if stage == "before-publication" {
					return barrier()
				}
				return writeBindingFile(path, data, func(dir *os.File) error {
					// Exercise the actual writer after replacement, not a mocked
					// success: this is the directory entry native admission waits for.
					published, err := os.ReadFile(path)
					if err != nil || string(published) != string(data) || dir.Name() != filepath.Dir(path) {
						t.Error("directory sync did not follow exact replacement", err)
					}
					return barrier()
				})
			}
			s.mu.Unlock()
			done := make(chan error, 1)
			go func() { _, err := d.w.StartWork(testContext(t), in); done <- err }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("journal barrier not reached")
			}
			d.f.mu.Lock()
			starts, sends := d.starts, d.sends
			d.f.mu.Unlock()
			close(release)
			if starts != 0 || sends != 0 {
				t.Fatal("native mutation preceded durable directory publication", starts, sends)
			}
			if err := <-done; !errors.Is(err, failure) {
				t.Fatal("storage failure lost", err)
			}
			// No native dispatch occurred. Storage recovery may safely admit the
			// same original ID; it must not permanently fence unrelated work.
			if view, err := d.w.StartWork(testContext(t), in); err != nil || view.Outcome != "accepted" {
				t.Fatal("definite pre-dispatch failure could not recover", view, err)
			}
			if _, err := d.w.StartWork(testContext(t), in); err != nil {
				t.Fatal(err)
			}
			d.f.mu.Lock()
			starts, sends = d.starts, d.sends
			d.f.mu.Unlock()
			if starts != 1 || sends != 1 {
				t.Fatal("safe retry duplicated native work", starts, sends)
			}
		})
	}
}

func TestWorkerJournalFailureAfterNativeThreadKeepsOriginalBinding(t *testing.T) {
	d := workerPair(t)
	s := d.w.engine
	failure := errors.New("fixture directory sync failed after native thread")
	writes := 0
	s.mu.Lock()
	s.writeBinding = func(path string, data []byte) error {
		writes++
		if writes == 2 {
			return writeBindingFile(path, data, func(*os.File) error { return failure })
		}
		return writeBindingFile(path, data, (*os.File).Sync)
	}
	s.mu.Unlock()
	in, view, err := d.start(t, 1)
	if !errors.Is(err, failure) || view.Outcome != "unknown" {
		t.Fatal("post-dispatch failure presented as definitive receipt", view, err)
	}
	d.source.next()
	if original, err := d.w.StartWork(testContext(t), in); err != nil || original.Outcome != "unknown" {
		t.Fatal("original unknown thread binding was discarded", original, err)
	}
	// The injected Sync failure leaves the replacement visible. Reopening it
	// must retain its exact native identity, not create a second native thread.
	reopened := NewWorker(WorkerOptions{Target: d.w.target, Directory: s.opts.Directory, Source: d.source})
	defer reopened.engine.cancelLife()
	if reopened.engine.loadErr != nil {
		t.Fatal(reopened.engine.loadErr)
	}
	if original, err := reopened.StartWork(testContext(t), in); err != nil || original.Outcome != "unknown" {
		t.Fatal("reopened original intent was redispatched", original, err)
	}
	d.f.mu.Lock()
	starts, sends := d.starts, d.sends
	d.f.mu.Unlock()
	if starts != 1 || sends != 0 {
		t.Fatal("post-dispatch storage failure duplicated or started native work", starts, sends)
	}
}

func TestWorkerJournalContinuationSyncFailureAllowsOriginalSafeRetry(t *testing.T) {
	d := workerPair(t)
	in, _, err := d.start(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	msg := api.TaskMessage{ID: in.ID, RequestID: "continuation-before-dispatch", Prompt: "Synthetic continuation", Source: in.Source, RequestDigest: strings.Repeat("b", 64)}
	failure := errors.New("fixture continuation directory sync failed")
	s := d.w.engine
	failed := false
	s.mu.Lock()
	s.writeBinding = func(path string, data []byte) error {
		var saved binding
		if err := json.Unmarshal(data, &saved); err != nil {
			return err
		}
		if !failed && saved.Tasks[in.ID].Requests[msg.RequestID].Outcome == "unknown" {
			failed = true
			return writeBindingFile(path, data, func(*os.File) error { return failure })
		}
		return writeBindingFile(path, data, (*os.File).Sync)
	}
	s.mu.Unlock()
	if _, err = d.w.SendWork(testContext(t), msg); !errors.Is(err, failure) {
		t.Fatal("pre-dispatch storage failure missing", err)
	}
	if d.w.WorkMessageRecorded(msg) {
		t.Fatal("definite non-dispatch was retained as a native receipt")
	}
	d.f.mu.Lock()
	sends := d.sends
	d.f.mu.Unlock()
	if sends != 1 {
		t.Fatal("continuation dispatched before durable publication", sends)
	}
	if view, err := d.w.SendWork(testContext(t), msg); err != nil || view.Outcome != "accepted" {
		t.Fatal("definite continuation non-dispatch could not recover", view, err)
	}
	d.source.next()
	if _, err = d.w.SendWork(testContext(t), msg); err != nil {
		t.Fatal("original continuation receipt lost", err)
	}
	d.f.mu.Lock()
	sends = d.sends
	d.f.mu.Unlock()
	if sends != 2 {
		t.Fatal("same original continuation duplicated native dispatch", sends)
	}
}

func TestWorkerJournalFailureRetainsUnknownContinuationAndNativeError(t *testing.T) {
	d := workerPair(t)
	in, _, err := d.start(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	msg := api.TaskMessage{ID: in.ID, RequestID: "original-continuation", Prompt: "Synthetic continuation", Source: in.Source, RequestDigest: strings.Repeat("b", 64)}
	failure := errors.New("fixture post-dispatch journal failure")
	d.f.mu.Lock()
	d.sendUnknown = true
	d.f.mu.Unlock()
	s := d.w.engine
	failed := false
	s.mu.Lock()
	s.writeBinding = func(path string, data []byte) error {
		var saved binding
		if err := json.Unmarshal(data, &saved); err != nil {
			return err
		}
		d.f.mu.Lock()
		dispatched := d.sends == 2
		d.f.mu.Unlock()
		if !failed && dispatched && saved.Tasks[in.ID].Requests[msg.RequestID].Outcome == "unknown" {
			failed = true
			return failure
		}
		return writeBindingFile(path, data, (*os.File).Sync)
	}
	s.mu.Unlock()
	if _, err = d.w.SendWork(testContext(t), msg); !errors.Is(err, ErrProtocol) || !errors.Is(err, failure) {
		t.Fatal("journal failure erased the uncertain native error", err)
	}
	d.source.next()
	if !d.w.WorkMessageRecorded(msg) {
		t.Fatal("unknown original continuation receipt discarded")
	}
	if _, err = d.w.SendWork(testContext(t), msg); err != nil {
		t.Fatal("original receipt required fresh authorization or dispatch", err)
	}
	d.f.mu.Lock()
	starts, sends := d.starts, d.sends
	d.f.mu.Unlock()
	if starts != 1 || sends != 2 {
		t.Fatal("post-dispatch continuation was resent", starts, sends)
	}
}

func TestWorkerStopCancelsOnlyExactOwnedTurnElicitationBeforeInterrupt(t *testing.T) {
	d := workerPair(t)
	in, _, err := d.start(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := d.start(t, 2)
	if err != nil {
		t.Fatal(err)
	}
	s := d.w.engine
	s.mu.Lock()
	thread, run := s.binding.Tasks[in.ID].Thread, s.binding.Tasks[in.ID].Run
	otherThread, otherRun := s.binding.Tasks[other.ID].Thread, s.binding.Tasks[other.ID].Run
	s.mu.Unlock()
	for _, request := range []struct{ id, thread, run string }{{"stop-current", thread, run}, {"keep-other-task", otherThread, otherRun}, {"keep-other-run", thread, run + "-other"}} {
		d.f.emit(wireMessage{ID: raw(request.id), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{
			"threadId": request.thread, "turnId": request.run, "mode": "form", "serverName": "fixture",
			"message": request.id, "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		})})
	}
	awaitState(t, s, func(api.Snapshot) bool { return len(d.w.WorkApprovals()) == 3 })
	var stopped api.WorkApproval
	for _, approval := range d.w.WorkApprovals() {
		if approval.Approval.Description == "stop-current" {
			stopped = approval
		}
	}
	d.f.mu.Lock()
	original := d.f.handle
	d.f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/interrupt" {
			select {
			case answer := <-d.f.answers:
				if string(answer.ID) != `"stop-current"` || string(answer.Result) != `{"_meta":null,"action":"cancel","content":null}` {
					t.Error("Stop cancelled another target or granted access", string(answer.ID), string(answer.Result))
				}
			default:
				return &NativeError{Code: -32000, Message: "native tool still awaits exact elicitation cancellation"}, true
			}
			s.mu.Lock()
			for _, prompt := range s.prompts {
				if prompt.view.Description != "stop-current" && prompt.view.Status != "pending" {
					t.Error("Stop changed another task/run approval", prompt.view.Description, prompt.view.Status)
				}
			}
			s.mu.Unlock()
			select {
			case answer := <-d.f.answers:
				t.Error("Stop emitted an unrelated cancellation", string(answer.ID))
			default:
			}
		}
		return original(m)
	}
	d.f.mu.Unlock()
	if _, err = d.w.StopWork(testContext(t), in.ID); err != nil {
		t.Fatal(err)
	}
	if err = d.w.DecideWork(testContext(t), stopped, api.Decision{ID: stopped.Approval.ID, Choice: "accept"}); err == nil {
		t.Fatal("stopped elicitation remained actionable")
	}
	awaitState(t, s, func(api.Snapshot) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.binding.Tasks[in.ID].View.Status == "interrupted"
	})
	if view, err := d.w.ReadWork(testContext(t), other.ID); err != nil || view.Status != "awaiting_approval" {
		t.Fatal("Stop interrupted another task or consumed its approval", view, err)
	}
	d.f.mu.Lock()
	interrupts := append([]map[string]string(nil), d.interrupts...)
	d.f.mu.Unlock()
	if len(interrupts) != 1 || interrupts[0]["threadId"] != thread || interrupts[0]["turnId"] != run {
		t.Fatal("Stop changed the native interrupt target", interrupts)
	}
}
