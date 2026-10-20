package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestArchivedWorkerHistoryReadsOriginalPagesWithoutAdoptingOwner(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint("unknown=", unknown), func(t *testing.T) {
			s, f, d, manager := taskPair(t)
			id, thread, run := legacyWorker(t, s, f, "completed")
			s.mu.Lock()
			task := s.binding.Tasks[id]
			task.View.Status = "completed"
			s.binding.LostOwnerTasks = map[string]bool{id: true}
			s.binding.LostOwners = []lostOwnerRecord{{Thread: "old-resident", TaskIDs: []string{id}}}
			if unknown {
				task.Pending = "original-uncertain"
				task.Requests[task.Pending] = taskReceipt{Outcome: "unknown", Phase: "dispatching", PriorRun: run}
			}
			if err := s.save(); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(s.binding)
			s.mu.Unlock()
			f.mu.Lock()
			native := f.workers[thread]
			native.Turns = nil
			for i := range 22 {
				turnID := fmt.Sprintf("original-run-%d", i)
				if i == 21 {
					turnID = run
				}
				native.Turns = append(native.Turns, nativeTurn{ID: turnID, Status: "completed", Items: []nativeItem{
					{ID: "user", Type: "userMessage", Content: []nativeInput{{Type: "text", Text: fmt.Sprintf("original input %d", i)}}},
					{ID: "answer", Type: "agentMessage", Text: fmt.Sprintf("original answer %d", i)},
				}})
			}
			f.workers[thread] = native
			var writes atomic.Int32
			previous := f.handle
			f.handle = func(m wireMessage) (any, bool) {
				switch m.Method {
				case "thread/start", "thread/resume", "turn/start", "turn/steer", "turn/interrupt", "thread/unsubscribe":
					writes.Add(1)
				}
				return previous(m)
			}
			f.mu.Unlock()
			if err := manager.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			var first []api.TaskConversationMessage
			for range 2 {
				got, err := manager.ReadTaskConversation(testContext(t), id)
				wantStatus := "completed"
				if unknown {
					wantStatus = "unknown"
				}
				if err != nil || got.Status != wantStatus || len(got.Messages) != 44 {
					t.Fatal("archived native history unavailable or uncertainty lost", got.Status, len(got.Messages), err)
				}
				if got.Messages[0].Text != "original input 0" || got.Messages[43].Text != "original answer 21" {
					t.Fatal("original FIFO history was replaced")
				}
				if first == nil {
					first = got.Messages
				} else {
					for i, message := range first {
						if message != got.Messages[i] {
							t.Fatal("repeat read changed original message identities")
						}
					}
				}
			}
			if _, err := s.SendWork(testContext(t), api.TaskMessage{ID: id, RequestID: "forbidden-new-input", Prompt: "do not execute"}); err == nil {
				t.Fatal("history read adopted the lost execution owner")
			}
			if _, err := s.WorkTerminal(testContext(t), id); err == nil {
				t.Fatal("history read permitted terminal attach to replacement owner")
			}
			s.mu.Lock()
			after, _ := json.Marshal(s.binding)
			s.mu.Unlock()
			durable, _ := os.ReadFile(s.opts.StateFile)
			var disk binding
			if err := json.Unmarshal(durable, &disk); err != nil {
				t.Fatal(err)
			}
			saved, _ := json.Marshal(disk)
			if !bytes.Equal(before, after) || !bytes.Equal(before, saved) || writes.Load() != 0 || d.starts != 0 || d.sends != 0 {
				t.Fatal("history read mutated original owner/receipts or dispatched work", writes.Load(), d.starts, d.sends)
			}
		})
	}
}

func TestArchivedWorkerUnreadableHistoryIsNotEmptySuccess(t *testing.T) {
	for _, failure := range []string{"missing", "permission", "empty", "wrong-run", "later-page"} {
		t.Run(failure, func(t *testing.T) {
			s, f, _, manager := taskPair(t)
			id, thread, _ := legacyWorker(t, s, f, "completed")
			s.mu.Lock()
			s.binding.Tasks[id].View.Status = "completed"
			s.binding.LostOwnerTasks = map[string]bool{id: true}
			s.mu.Unlock()
			f.mu.Lock()
			previous := f.handle
			f.handle = func(m wireMessage) (any, bool) {
				var p struct{ ThreadID, ItemsView, Cursor string }
				_ = json.Unmarshal(m.Params, &p)
				if p.ThreadID == thread {
					if m.Method == "thread/read" && (failure == "missing" || failure == "permission") {
						return &NativeError{Code: -32600, Message: failure + ": " + thread}, true
					}
					if m.Method == "thread/turns/list" && p.ItemsView == "full" {
						if failure == "empty" {
							return turnPage{Data: []nativeTurn{}}, true
						}
						if failure == "later-page" && p.Cursor != "" {
							return &NativeError{Code: -32603, Message: "original page unavailable"}, true
						}
						page := turnPage{Data: []nativeTurn{{ID: "another-run", Status: "completed", Items: []nativeItem{{ID: "answer", Type: "agentMessage", Text: "older answer"}}}}}
						if failure == "later-page" {
							page.NextCursor = "original-next"
						}
						return page, true
					}
				}
				return previous(m)
			}
			f.mu.Unlock()
			if err := manager.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			if got, err := manager.ReadTaskConversation(testContext(t), id); err == nil || len(got.Messages) != 0 {
				t.Fatal("missing/partial original history was presented as success", err)
			}
		})
	}
}
