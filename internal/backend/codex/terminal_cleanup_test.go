package codex

import (
	"encoding/json"
	"sync/atomic"
	"testing"
)

func TestUnloadedChildCleanupRequiresOriginalNativeIdleProof(t *testing.T) {
	for _, name := range []string{
		"not-loaded", "not-found", "wrong-error-id", "wrong-code", "unrelated-error",
		"explicit-target", "native-active", "native-idle", "native-unknown",
		"active-turn", "wrong-run", "missing-turn", "read-failed", "wrong-read-id",
		"pending-input", "unknown-receipt", "active-child", "approval",
		"cleanup-target", "stop-run", "unknown-stop", "working-task",
		"late-revision", "partial-terminal-list",
	} {
		t.Run(name, func(t *testing.T) {
			s, f, _, _ := taskPair(t)
			id, thread, run := legacyWorker(t, s, f, "completed")
			s.mu.Lock()
			task := s.binding.Tasks[id]
			task.View.Status = "completed"
			s.children[thread] = true
			switch name {
			case "pending-input":
				task.Pending = "original-pending"
			case "unknown-receipt":
				task.Requests["original-unknown"] = taskReceipt{Outcome: "unknown"}
			case "active-child":
				s.childRuns[thread] = ""
			case "approval":
				s.prompts["original-review"] = &prompt{thread: thread}
			case "cleanup-target":
				s.binding.CleanupTargets = []string{thread}
			case "stop-run":
				s.binding.StopRuns = map[string]string{thread: run}
			case "unknown-stop":
				task.StopPendingRequest, task.StopPendingTurn = "original-stop", run
			case "working-task":
				task.View.Status = "working"
			}
			s.mu.Unlock()
			f.mu.Lock()
			native := f.workers[thread]
			switch name {
			case "native-active":
				native.Status.Type = "active"
			case "native-idle":
				native.Status.Type = "idle"
			case "native-unknown":
				native.Status.Type = "unrecognized"
			case "active-turn":
				native.Turns[0].Status = "inProgress"
			case "wrong-run":
				native.Turns[0].ID = "another-run"
			case "missing-turn":
				native.Turns = nil
			}
			f.workers[thread] = native
			var reads atomic.Int32
			previous := f.handle
			f.handle = func(m wireMessage) (any, bool) {
				var p struct{ ThreadID, Cursor string }
				_ = json.Unmarshal(m.Params, &p)
				if p.ThreadID == thread {
					switch m.Method {
					case "thread/backgroundTerminals/list":
						if name == "partial-terminal-list" && p.Cursor == "" {
							return map[string]any{"data": []map[string]string{{"processId": "unconfirmed-process"}}, "nextCursor": "next"}, true
						}
						code, message := -32600, "thread not found: "+thread
						switch name {
						case "not-loaded":
							message = "thread not loaded: " + thread
						case "wrong-error-id":
							message += "-other"
						case "wrong-code":
							code = -32000
						case "unrelated-error":
							message = "permission denied"
						}
						return &NativeError{Code: code, Message: message}, true
					case "thread/read":
						reads.Add(1)
						switch name {
						case "read-failed":
							return &NativeError{Code: -32600, Message: "no rollout found for thread id " + thread}, true
						case "wrong-read-id":
							return map[string]any{"thread": nativeThread{ID: "other"}}, true
						case "late-revision":
							s.mu.Lock()
							s.childRevision[thread]++
							s.mu.Unlock()
						}
					}
				}
				return previous(m)
			}
			f.mu.Unlock()
			var targets []string
			if name == "explicit-target" {
				targets = []string{thread}
			}
			err := s.cleanTerminals(testContext(t), s.client, targets...)
			wantSuccess := name == "not-loaded" || name == "not-found"
			if (err == nil) != wantSuccess {
				t.Fatal("incorrect missing-thread cleanup decision", err)
			}
			if wantSuccess && reads.Load() != 1 {
				t.Fatal("cleanup did not read the original native thread")
			}
			if name == "partial-terminal-list" && reads.Load() != 0 {
				t.Fatal("partially enumerated processes were treated as absent")
			}
			// A read-only exception cannot discard retained uncertainty.
			s.mu.Lock()
			if name == "unknown-receipt" && task.Requests["original-unknown"].Outcome != "unknown" ||
				name == "cleanup-target" && len(s.binding.CleanupTargets) != 1 ||
				name == "approval" && len(s.prompts) != 1 {
				t.Error("cleanup erased original uncertainty")
			}
			s.mu.Unlock()
		})
	}
}

func TestUnloadedCurrentThreadStillRequiresCleanup(t *testing.T) {
	for _, prefix := range []string{"thread not loaded: ", "thread not found: "} {
		t.Run(prefix, func(t *testing.T) {
			s, f := sessionPair(t, "")
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method == "thread/backgroundTerminals/list" {
					return &NativeError{Code: -32600, Message: prefix + "thread-native"}, true
				}
				return nil, false
			}
			f.mu.Unlock()
			if err := s.cleanTerminals(testContext(t), s.client); err == nil {
				t.Fatal("current-thread error was treated as historical cleanup")
			}
		})
	}
}
