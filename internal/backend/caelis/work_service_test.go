package caelis

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func addServiceWorker(s *Session, id, sid string, head wire.SessionState) {
	head.SessionId = sid
	s.state.Workers[id] = worker{Native: true, Task: api.Task{ID: id, Status: "working", Outcome: "accepted"}, Binding: wire.SessionBinding{SessionId: sid, PrincipalId: "owner"}}
	s.state.Views[sid] = &view{State: head, Items: []api.Item{}, Seen: map[string]bool{}}
}
func nativeWorkHead(turn string) wire.SessionState {
	return wire.SessionState{SessionId: "work", Run: wire.RunState{Active: pointer(true), HandleId: pointer("h"), RunId: pointer("r-" + turn), TurnId: &turn, Status: pointer("working")}}
}
func TestWorkServiceStopFencesNativeGenerationAndOriginalReceipt(t *testing.T) {
	var posts atomic.Int32
	head := nativeWorkHead("one")
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/state"):
			writeFixture(w, head)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/cancel"):
			posts.Add(1)
			var body wire.CancelRequest
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Target.TurnId != "one" {
				t.Error("cancel lost exact original target")
			}
			writeFixture(w, wire.CommandResult{OperationId: value(body.OperationId), Outcome: "accepted"})
		default:
			t.Errorf("unexpected native call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	addServiceWorker(s, "task", "work", head)
	oldKey := workStopExecutionKey(head)
	in := api.WorkStop{ID: "task", RequestID: "stop-original", ExecutionKey: oldKey}
	if _, err := s.StopWorkRequest(t.Context(), in); err != nil || posts.Load() != 1 {
		t.Fatal("original stop", err, posts.Load())
	}
	head.Run.HandleId = pointer("replacement-handle")
	s.state.Views["work"].State = head
	// TurnId alone is a completion identity, not a native cancellation target.
	// Even after observation sees a replacement handle, the issued old stop
	// target may not acquire that handle under the same turn.
	if _, err := s.StopWorkRequest(t.Context(), api.WorkStop{ID: "task", RequestID: "late-same-turn", ExecutionKey: oldKey}); err == nil || posts.Load() != 1 {
		t.Fatal("same-turn replacement owner was interrupted")
	}
	head = nativeWorkHead("two")
	// The accepted receipt remains authoritative even after another turn. It may
	// be read, but retry cannot cancel the new turn.
	if _, err := s.StopWorkRequest(t.Context(), in); err != nil || posts.Load() != 1 {
		t.Fatal("same stop was replayed", err, posts.Load())
	}
	if _, err := s.StopWorkRequest(t.Context(), api.WorkStop{ID: "task", RequestID: "late", ExecutionKey: oldKey}); err == nil || posts.Load() != 1 {
		t.Fatal("late stop interrupted subsequent turn")
	}
	q := api.WorkOperationQuery{TaskID: "task", RequestID: in.RequestID, Kind: "stop", Fingerprint: api.WorkFingerprint(in)}
	result, err := s.ReadWorkOperation(t.Context(), q)
	if err != nil || result.Outcome != "accepted" {
		t.Fatal("original receipt unreadable", result, err)
	}
	q.Fingerprint = "different"
	if _, err = s.ReadWorkOperation(t.Context(), q); err == nil {
		t.Fatal("conflicting digest accepted")
	}
}
func TestWorkServiceUnknownStopNeverRedispatches(t *testing.T) {
	var posts atomic.Int32
	head := nativeWorkHead("one")
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/state"):
			writeFixture(w, head)
		case r.Method == "POST":
			posts.Add(1)
			drop(w)
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/operations/"):
			op := strings.TrimPrefix(r.URL.Path, "/api/control/v1/operations/")
			writeFixture(w, wire.CommandResult{OperationId: op, Outcome: "unknown"})
		default:
			t.Errorf("unexpected native call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	addServiceWorker(s, "task", "work", head)
	in := api.WorkStop{ID: "task", RequestID: "uncertain", ExecutionKey: workStopExecutionKey(head)}
	if _, err := s.StopWorkRequest(t.Context(), in); err == nil {
		t.Fatal("lost response fabricated accepted stop")
	}
	q := api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "stop", Fingerprint: api.WorkFingerprint(in)}
	result, err := s.ReadWorkOperation(t.Context(), q)
	if err != nil || result.Outcome != "unknown" {
		t.Fatal("unknown was replaced", result, err)
	}
	if _, err = s.StopWorkRequest(t.Context(), in); err == nil || posts.Load() != 1 {
		t.Fatal("unknown replayed", err, posts.Load())
	}
}

func TestWorkServiceUnknownDecisionCannotBeReplacedWithAnotherOperation(t *testing.T) {
	var posts atomic.Int32
	head := nativeWorkHead("one")
	head.Approval.Active = testApproval()
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/state"):
			writeFixture(w, head)
		case r.Method == "POST":
			posts.Add(1)
			drop(w)
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/operations/"):
			op := strings.TrimPrefix(r.URL.Path, "/api/control/v1/operations/")
			writeFixture(w, wire.CommandResult{OperationId: op, Outcome: "unknown"})
		default:
			t.Errorf("unexpected native call %s %s", r.Method, r.URL.Path)
		}
	})
	addServiceWorker(s, "task", "work", head)
	observed, err := s.ObserveWork(t.Context(), "task")
	if err != nil {
		t.Fatal(err)
	}
	in := api.WorkDecision{TaskID: "task", RequestID: "original-decision", Decision: api.Decision{ID: observed.Approvals[0].ID, Choice: "native-allow"}}
	if err = s.DecideWork(t.Context(), in); err == nil {
		t.Fatal("lost native decision became success")
	}
	q := api.WorkOperationQuery{TaskID: in.TaskID, RequestID: in.RequestID, Kind: "decision", Fingerprint: api.WorkFingerprint(in)}
	receipt, err := s.ReadWorkOperation(t.Context(), q)
	if err != nil || receipt.Outcome != "unknown" {
		t.Fatal(receipt, err)
	}
	in.RequestID = "replacement-must-not-run"
	if err = s.DecideWork(t.Context(), in); err == nil || posts.Load() != 1 {
		t.Fatal("new host operation bypassed original uncertain decision", err, posts.Load())
	}
}
func TestWorkServiceObservationAndDecisionStayTaskScoped(t *testing.T) {
	approval := testApproval()
	head := nativeWorkHead("two")
	head.Approval.Active = approval
	// Approval target is turn one: a native request may remain pending across a
	// later head. Decision follows the original request, not the new turn.
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			writeFixture(w, head)
			return
		}
		posts.Add(1)
		var body wire.ResolveApprovalRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.ApprovalRequestId != approval.RequestId || body.Target.TurnId != "t" || value(body.OptionId) != "native-allow" {
			t.Error("native decision target changed")
		}
		writeFixture(w, wire.CommandResult{OperationId: value(body.OperationId), Outcome: "accepted"})
	})
	addServiceWorker(s, "task", "work", head)
	addServiceWorker(s, "other", "different", nativeWorkHead("else"))
	s.state.Views["work"].Items = []api.Item{{ID: "partial", TurnKey: "two", Kind: "assistant", Status: "inProgress", Text: "live output"}}
	out, err := s.ObserveWork(t.Context(), "task")
	if err != nil || len(out.Items) != 1 || out.Items[0].Status != "inProgress" || len(out.Approvals) != 1 || out.Approvals[0].TaskID != "task" || out.Approvals[0].NativeID != approval.RequestId || out.Approvals[0].Details == "" {
		t.Fatal("full scoped native view lost", out, err)
	}
	d := api.WorkDecision{TaskID: "other", RequestID: "wrong-source", Decision: api.Decision{ID: out.Approvals[0].ID, Choice: "native-allow"}}
	if err = s.DecideWork(t.Context(), d); err == nil || posts.Load() != 0 {
		t.Fatal("another task controlled original native approval")
	}
	d.TaskID = "task"
	d.RequestID = "decision-original"
	if err = s.DecideWork(t.Context(), d); err != nil || posts.Load() != 1 {
		t.Fatal("native decision failed", err, posts.Load())
	}
	if err = s.DecideWork(t.Context(), d); err != nil || posts.Load() != 1 {
		t.Fatal("decision receipt replayed", err, posts.Load())
	}
	caps, _ := s.WorkCapabilities(t.Context(), "task")
	if caps.URL || caps.PendingAcrossRestart {
		t.Fatal("unverified native capability advertised")
	}
}
func TestWorkServiceFileProofAndTransferUseExactWorker(t *testing.T) {
	var reads atomic.Int32
	bytes := []byte("worker output")
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if r.URL.Path != "/api/control/v1/sessions/work/resources/out/content" {
			t.Errorf("wrong native file scope %s", r.URL.Path)
		}
		writeFixture(w, wire.SessionResourceContent{Resource: wire.SessionResource{SessionId: "work", Id: "out", Name: "result.txt", MediaType: "text/plain", Size: len(bytes), Sha256: digest(bytes)}, Data: base64.StdEncoding.EncodeToString(bytes)})
	})
	addServiceWorker(s, "task", "work", nativeWorkHead("one"))
	addServiceWorker(s, "other", "other-native", nativeWorkHead("two"))
	s.state.Views["work"].Items = []api.Item{{ID: "file", Artifacts: []api.Artifact{{ID: "resource:work:out", Name: "result.txt"}}}}
	file, err := s.ReadWorkFile(t.Context(), "task", "resource:work:out")
	if err != nil || string(file.Data) != string(bytes) || file.Name != "result.txt" {
		t.Fatal(file, err)
	}
	// Even a tampered local projection cannot turn another Session path into a
	// file reference for this Worker.
	s.state.Views["other-native"].Items = []api.Item{{Artifacts: []api.Artifact{{ID: "resource:work:out"}}}}
	if _, err = s.ReadWorkFile(t.Context(), "other", "resource:work:out"); err == nil || reads.Load() != 1 {
		t.Fatal("cross task file read escaped scope")
	}
}
func TestWorkServiceInputsUseWorkerNativeEncoderAndBindBytes(t *testing.T) {
	var uploads, prompts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/control/v1/sessions/work/resources":
			uploads.Add(1)
			var body wire.SessionResourceRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			data, _ := base64.StdEncoding.DecodeString(body.Data)
			if value(body.SessionId) != "work" {
				t.Error("used resident resource upload")
			}
			writeFixture(w, wire.CommandResult{OperationId: value(body.OperationId), Outcome: "committed", SessionResource: &wire.SessionResource{Id: "input", SessionId: "work", Name: body.Name, MediaType: body.MediaType, Size: len(data), Sha256: digest(data)}})
		case "/api/control/v1/sessions/work/prompt":
			prompts.Add(1)
			var body wire.PromptRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.ContentParts) != 1 || !strings.Contains(value(body.ContentParts[0].Text), "input") {
				t.Error("attachment native resource reference absent")
			}
			writeFixture(w, wire.CommandResult{OperationId: value(body.OperationId), Outcome: "accepted"})
		default:
			t.Errorf("unexpected native input call %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	addServiceWorker(s, "task", "work", wire.SessionState{})
	path := filepath.Join(t.TempDir(), "data.txt")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	in := api.TaskMessage{ID: "task", RequestID: "send-with-file", Prompt: "read attached", Files: []api.InputFile{{Name: "data.txt", Path: path}}}
	digests, err := api.WorkInputDigests(in.Files)
	if err != nil {
		t.Fatal(err)
	}
	in.FileDigests = digests
	ctx := (&WorkOwner{s}).source(t.Context(), in.RequestID)
	if _, err = s.SendWork(ctx, in); err != nil || uploads.Load() != 1 || prompts.Load() != 1 {
		t.Fatal("worker input failed", err, uploads.Load(), prompts.Load())
	}
	query := api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "send", Fingerprint: api.WorkFingerprint(in)}
	if receipt, err := s.ReadWorkOperation(t.Context(), query); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("file intent receipt", receipt, err)
	}
	if err = os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SendWork(ctx, in); err == nil || prompts.Load() != 1 {
		t.Fatal("same request accepted changed file bytes")
	}
}
func TestWorkOwnerSourceFenceAndNativeBuild(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "source-owner")
	w, err := NewWorkOwner(Options{Directory: dir, WorkSourceIdentity: "authenticated-peer-one"})
	if err != nil {
		t.Fatal(err)
	}
	w.engine.state.Workers["task"] = worker{Task: api.Task{ID: "task"}}
	if err = w.engine.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if _, err = NewWorkOwner(Options{Directory: dir, WorkSourceIdentity: "authenticated-peer-two"}); err == nil {
		t.Fatal("original source identity replaced")
	}
	w.engine.info = wire.ServerInfo{BuildId: pointer("native-original-build"), DistributionVersion: pointer("native-distribution")}
	caps, err := w.WorkCapabilities(t.Context(), "task")
	if err != nil || caps.Build != "native-original-build" || caps.Runtime != "caelis" {
		t.Fatal("App version replaced owner capability", caps, err)
	}
	inputs := []workerInput{{Name: "data.txt", Data: []byte("bytes")}}
	path := filepath.Join(t.TempDir(), "data.txt")
	_ = os.WriteFile(path, inputs[0].Data, 0600)
	want, _ := api.WorkInputDigests([]api.InputFile{{Name: "data.txt", Path: path}})
	if !slices.Equal(workerInputDigests(inputs), want) {
		t.Fatal("native and host file digest conventions differ")
	}
}

func TestWorkServicePreparedStartRetainsFilesAndUnknownReceipt(t *testing.T) {
	var creates, prompts, uploads atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/control/v1/sessions":
			creates.Add(1)
			var body wire.CreateSessionRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeFixture(w, wire.CommandResult{OperationId: value(body.OperationId), Outcome: "accepted", SessionId: pointer("work")})
		case "/api/control/v1/sessions/work/state":
			writeFixture(w, wire.SessionState{SessionId: "work"})
		case "/api/control/v1/sessions/work/resources":
			uploads.Add(1)
			var body wire.SessionResourceRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			data, _ := base64.StdEncoding.DecodeString(body.Data)
			writeFixture(w, wire.CommandResult{OperationId: value(body.OperationId), Outcome: "committed", SessionResource: &wire.SessionResource{Id: "input", SessionId: "work", Name: body.Name, MediaType: body.MediaType, Size: len(data), Sha256: digest(data)}})
		case "/api/control/v1/sessions/work/prompt":
			prompts.Add(1)
			drop(w)
		default:
			if strings.Contains(r.URL.Path, "/operations/") {
				op := strings.TrimPrefix(r.URL.Path, "/api/control/v1/operations/")
				writeFixture(w, wire.CommandResult{OperationId: op, Outcome: "unknown"})
				return
			}
			t.Errorf("unexpected native start call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	file := filepath.Join(t.TempDir(), "input.txt")
	_ = os.WriteFile(file, []byte("frozen bytes"), 0600)
	in := api.WorkStart{TaskStart: api.TaskStart{RequestID: "create-original", Title: "work", Prompt: "read file", Files: []api.InputFile{{Name: "input.txt", Path: file}}}, ID: "task", Workspace: t.TempDir()}
	in.FileDigests, _ = api.WorkInputDigests(in.Files)
	ctx := (&WorkOwner{s}).source(t.Context(), in.RequestID)
	if task, err := s.StartWork(ctx, in); err == nil || task.Outcome != "unknown" {
		t.Fatal("unknown native start fabricated success", task, err)
	}
	saved, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	pending := saved.Workers[in.ID]
	if pending.Start == nil || len(pending.Start.Files) != 1 || string(pending.Start.Files[0].Data) != "frozen bytes" {
		t.Fatal("pending start depends on temporary transport file")
	}
	_ = os.Remove(file)
	query := api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "start", Fingerprint: api.WorkFingerprint(in)}
	receipt, err := s.ReadWorkOperation(t.Context(), query)
	if err != nil || receipt.Outcome != "unknown" || creates.Load() != 1 || prompts.Load() != 1 || uploads.Load() != 1 {
		t.Fatal("original unknown start replaced or replayed", receipt, err, creates.Load(), prompts.Load(), uploads.Load())
	}
	// Recovery can reconcile the existing original command, but cannot POST it.
	if task, err := s.advanceWorker(t.Context(), pending); task.Outcome != "unknown" || creates.Load() != 1 || prompts.Load() != 1 || uploads.Load() != 1 {
		t.Fatal("prepared native start replayed", task, err, creates.Load(), prompts.Load(), uploads.Load())
	}
}
func TestWorkServiceHistoryPreservesCanonicalArtifacts(t *testing.T) {
	resource := wire.SessionResource{SessionId: "work", Id: "output", Name: "report.txt"}
	result, _ := json.Marshal(map[string]any{"resource": resource})
	update, _ := json.Marshal(map[string]any{"sessionUpdate": "tool_call_update", "name": "PublishArtifact", "status": "completed", "rawOutput": map[string]string{"result": string(result)}})
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		workerHistoryFrames(w, []wire.Envelope{{Kind: "session/update", SessionId: pointer("work"), TurnId: pointer("t"), Update: pointer(json.RawMessage(update))}}, "")
	})
	addServiceWorker(s, "task", "work", nativeWorkHead("t"))
	history, err := s.ReadWorkConversation(t.Context(), "task")
	if err != nil || len(history) != 1 || len(history[0].Artifacts) != 1 || history[0].Artifacts[0].ID != "resource:work:output" {
		t.Fatal("canonical native artifact disappeared from history", history, err)
	}
}

func TestWorkServiceOpaqueRequestRemainsPendingAndInspectable(t *testing.T) {
	head := nativeWorkHead("one")
	head.Approval.Active = &wire.ActiveApproval{RequestId: "opaque-native", Target: &wire.TurnTarget{HandleId: "h", RunId: "r", TurnId: "one"}, Permission: map[string]any{"native_future_request": map[string]any{"sensitive": true, "challenge": "opaque-marker"}}}
	var writes atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
			t.Error("unsupported native request was written")
		}
		writeFixture(w, head)
	})
	addServiceWorker(s, "task", "work", head)
	out, err := s.ObserveWork(t.Context(), "task")
	if err != nil || len(out.Approvals) != 1 || !strings.Contains(out.Approvals[0].Details, `"state": "opaque"`) || !strings.Contains(out.Approvals[0].Details, `"sensitive_input": true`) || strings.Contains(out.Approvals[0].Details, "opaque-marker") || out.Approvals[0].NativeID != "opaque-native" {
		t.Fatal("opaque presence lost or private future payload exposed", out.Approvals, err)
	}
	decision := api.WorkDecision{TaskID: "task", RequestID: "unsupported-decision", Decision: api.Decision{ID: out.Approvals[0].ID, Choice: "allow"}}
	if err = s.DecideWork(t.Context(), decision); err == nil || writes.Load() != 0 {
		t.Fatal("unsupported request fabricated approval", err, writes.Load())
	}
	if s.state.Views["work"].State.Approval.Active == nil {
		t.Fatal("unsupported request was silently closed")
	}
}

func TestReadWorkOperationRejectsPartiallyLoadedBinding(t *testing.T) {
	q := api.WorkOperationQuery{TaskID: "task", RequestID: "original-request", Kind: "send", Fingerprint: "original-digest"}
	intent := workIntent{q.TaskID, q.RequestID, q.Kind, q.Fingerprint}
	op := "work-send-" + digest([]byte(q.RequestID))
	s := &Session{loadErr: errors.New("unreadable-original-binding"), state: binding{Workers: map[string]worker{"task": {Task: api.Task{ID: "task"}}}, Operations: map[string]journal{op: {Work: &intent, Outcome: "accepted"}}}}
	if got, err := s.ReadWorkOperation(t.Context(), q); err == nil || got.Outcome == "accepted" {
		t.Fatal("partial binding certified receipt", got, err)
	}
	s.loadErr = nil
	if got, err := s.ReadWorkOperation(t.Context(), q); err != nil || got.Outcome != "accepted" {
		t.Fatal("valid offline receipt unavailable", got, err)
	}
}
