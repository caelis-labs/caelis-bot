package machines

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func peerValidationFiles(t *testing.T, count, size int) []api.InputFile {
	t.Helper()
	dir := t.TempDir()
	files := make([]api.InputFile, count)
	for i := range files {
		name := "material-" + strconv.Itoa(i) + ".csv"
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, size), 0600); err != nil {
			t.Fatal(err)
		}
		files[i] = api.InputFile{Name: name, Path: path}
	}
	return files
}

func TestBotPeerCaelisInputValidationRejectsBeforeExecution(t *testing.T) {
	for _, tc := range []struct {
		name        string
		count, size int
	}{{"five files", 5, 16}, {"nine MiB", 1, 9 << 20}} {
		t.Run(tc.name, func(t *testing.T) {
			target := newDualPeerTarget(t)
			service, manager, local, machine, _ := dualPeerConnect(t, target)
			target.setDefault("caelis")
			tooMany := peerValidationFiles(t, tc.count, tc.size)
			result, err := manager.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "rejected-before-create", Prompt: "must not create native work", Files: tooMany})
			var rejected *api.WorkMessageRejection
			if err == nil || result.Outcome != "rejected" || !errors.As(err, &rejected) {
				t.Fatal("known native input refusal became an unknown execution", result, err)
			}
			owner := target.owner(service.state.ProfileID, "caelis")
			if owner == nil || owner.fileChecks.Load() != 0 {
				t.Fatal("metadata refusal read/dispatched file bytes or did not select B actual owner")
			}
			owner.mu.Lock()
			starts, states, receipts := owner.starts, len(owner.states), len(owner.receipts)
			owner.mu.Unlock()
			if starts != 0 || states != 0 || receipts != 0 || len(manager.ListTasks()) != 0 || len(service.state.Reservations) != 0 || len(service.state.Routes) != 0 || len(service.state.Runtimes) != 0 {
				t.Fatal("pre-dispatch refusal created task/receipt or stranded A preparation", starts, states, receipts)
			}
			connection := service.state.Profiles[machine].Peer
			remote, err := workerpeer.Call(t.Context(), connection, workerpeer.Request{Action: "states"})
			if err != nil || len(remote.States) != 0 {
				t.Fatal("B retained an undispatched execution binding", len(remote.States), err)
			}
			good := peerValidationFiles(t, 1, 24)
			task, err := manager.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "valid-csv-create", Prompt: "read the ordinary CSV", Files: good})
			if err != nil || task.Outcome != "accepted" || task.ID == "" {
				t.Fatal("invalid preparation blocked subsequent valid work", task, err)
			}
			result, err = manager.SendTask(t.Context(), api.TaskMessage{ID: task.ID, RequestID: "rejected-before-continue", Prompt: "must not continue native work", Files: tooMany})
			if err == nil || result.Outcome != "rejected" || !errors.As(err, &rejected) {
				t.Fatal("known continuation input refusal became unknown", result, err)
			}
			owner.mu.Lock()
			starts, sends, states, receipts := owner.starts, owner.sends, len(owner.states), len(owner.receipts)
			_, falselyRecorded := owner.receipts["rejected-before-continue"]
			owner.mu.Unlock()
			retained, readErr := manager.ReadTask(t.Context(), task.ID)
			if starts != 1 || sends != 0 || states != 1 || receipts != 1 || falselyRecorded || readErr != nil || retained.ID != task.ID || retained.Outcome != "accepted" || retained.Status != "working" || len(manager.ListTasks()) != 1 || local.mutations.Load() != 0 {
				t.Fatal("rejected files altered the original task/native execution", starts, sends, states, receipts, retained, readErr)
			}
			if _, err := manager.SendTask(t.Context(), api.TaskMessage{ID: task.ID, RequestID: "valid-after-file-refusal", Prompt: "continue with valid bytes", Files: good}); err != nil {
				t.Fatal("read-only refusal blocked later valid continuation", err)
			}
		})
	}
}

func TestBotPeerInputValidationDoesNotReplaceUnknownOriginalOperations(t *testing.T) {
	target := newDualPeerTarget(t)
	service, manager, local, machine, _ := dualPeerConnect(t, target)
	target.setDefault("caelis")
	files := peerValidationFiles(t, 1, 32)
	start := api.TaskStart{Machine: machine, RequestID: "uncertain-original-create-with-files", Prompt: "read the original bytes", Files: files}
	target.dropStart.Store(true)
	task, err := manager.StartTask(t.Context(), start)
	if err == nil || task.ID == "" {
		t.Fatal("lost response was not an uncertain original task", task, err)
	}
	owner := target.owner(service.state.ProfileID, "caelis")
	if owner == nil {
		t.Fatal("original native owner missing")
	}
	before := owner.fileChecks.Load()
	if before == 0 {
		t.Fatal("native dispatch did not validate original input bytes")
	}
	receipt, err := manager.ReconcileTaskRequest(t.Context(), task.ID, start.RequestID)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal("file preflight replaced original create receipt", receipt, err)
	}
	if again, err := manager.StartTask(t.Context(), start); err != nil || again.ID != task.ID || owner.fileChecks.Load() != before {
		t.Fatal("duplicate create was revalidated instead of retaining original operation", again, err)
	}
	// Drop the response only after B commits the original send. The replacement
	// fixture endpoint keeps the same authenticated peer and execution owner.
	var drop atomic.Bool
	drop.Store(true)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"call" && drop.Load() {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			var request workerpeer.Request
			_ = json.Unmarshal(body, &request)
			if request.Action == "send" && drop.CompareAndSwap(true, false) {
				target.peer.ServeHTTP(httptest.NewRecorder(), r)
				if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					_ = conn.Close()
				}
				return
			}
		}
		target.peer.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	if _, err := service.ConnectBot(t.Context(), api.BotConnectionInput{ID: machine, Address: proxy.URL, Name: "Machine B"}); err != nil {
		t.Fatal("failure-injection endpoint did not retain original peer", err)
	}
	in := api.TaskMessage{ID: task.ID, RequestID: "uncertain-original-send-with-files", Prompt: "continue using the original bytes", Files: files}
	if result, err := manager.SendTask(t.Context(), in); err == nil || result.Outcome != "unknown" {
		t.Fatal("lost send response did not retain original uncertainty", result, err)
	}
	before = owner.fileChecks.Load()
	if result, err := manager.SendTask(t.Context(), in); err != nil || result.ID != task.ID || result.Outcome != "accepted" || owner.fileChecks.Load() != before {
		t.Fatal("duplicate unknown send revalidated/replayed instead of reading original receipt", result, err)
	}
	owner.mu.Lock()
	starts, sends, receipts := owner.starts, owner.sends, len(owner.receipts)
	owner.mu.Unlock()
	if starts != 1 || sends != 1 || receipts != 2 || local.mutations.Load() != 0 || service.state.Runtimes[task.ID] != "caelis" {
		t.Fatal("reconciliation introduced replacement work, owner or native write", starts, sends, receipts)
	}
}
