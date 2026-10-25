package caelis

import (
	"bytes"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type auditSourceTransport struct {
	ids     []string
	creates map[string]string
	bodies  map[string]string
}

func (rt *auditSourceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var result any
	op := r.Header.Get("Idempotency-Key")
	if op != "" {
		rt.ids = append(rt.ids, op)
		if rt.bodies == nil {
			rt.bodies = map[string]string{}
		}
		b, _ := io.ReadAll(r.Body)
		if old, ok := rt.bodies[op]; ok && old != string(b) {
			return &http.Response{StatusCode: 409, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":"operation_conflict"}`))}, nil
		}
		rt.bodies[op] = string(b)
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/control/v1/sessions":
		sid := rt.creates[op]
		if sid == "" {
			sid = "native-" + op
			rt.creates[op] = sid
		}
		result = wire.CommandResult{OperationId: op, Outcome: "accepted", SessionId: &sid}
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/state"):
		sid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/control/v1/sessions/"), "/state")
		result = wire.SessionState{SessionId: sid}
	default:
		result = wire.CommandResult{OperationId: op, Outcome: "accepted"}
	}
	b, _ := json.Marshal(result)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b))}, nil
}

func TestForeignSourcesFreezeDistinctNativeOperationsAndOriginalReceipts(t *testing.T) {
	rt := &auditSourceTransport{creates: map[string]string{}}
	client := &client{origin: "http://127.0.0.1:12345", token: "SYNTHETIC_B_PRINCIPAL", http: &http.Client{Transport: rt}}
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]bool{}
	for _, source := range []string{"source-one", "source-two", "source-one"} {
		dir := filepath.Join(t.TempDir(), "private") // repeated source also exercises separate B directories
		opts := Options{Directory: dir, WorkSourceIdentity: source}
		owner, e := NewWorkOwner(opts)
		if e != nil {
			t.Fatal(e)
		}
		s := owner.engine
		s.client = client
		s.connected = true
		s.state.PrincipalID = "same-B-principal"
		in := api.WorkStart{ID: "task-shared-native-id", Workspace: workspace, TaskStart: api.TaskStart{RequestID: "same-original-request", Workspace: workspace, Title: "same assignment", Prompt: "same assignment"}, ExecutionOverride: &api.WorkExecutionSettings{Model: "controlled"}}
		if _, e = owner.StartWork(t.Context(), in); e != nil {
			t.Fatal(e)
		}
		w := s.state.Workers[in.ID]
		if w.NativeScope == "" || bindings[w.Binding.SessionId] {
			t.Fatal("sources adopted same native session", w.Binding, rt.ids)
		}
		bindings[w.Binding.SessionId] = true
		for _, kind := range []string{"create", "prompt", "send", "stop", "decision"} {
			if w.operationID(kind, in.RequestID) == (worker{}).operationID(kind, in.RequestID) {
				t.Fatal("unscoped native operation", kind)
			}
		}
		if w.HostStart == nil || *w.HostStart != (workIntent{in.ID, in.RequestID, "start", api.WorkFingerprint(in)}) {
			t.Fatal("host identity changed", w.HostStart)
		}
		reopened, e := NewWorkOwner(opts)
		if e != nil {
			t.Fatal(e)
		}
		restored := reopened.engine.state.Workers[in.ID]
		if restored.NativeScope != w.NativeScope || restored.Binding.SessionId != w.Binding.SessionId {
			t.Fatal("reopen changed original scope", restored, w)
		}
		q := api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "start", Fingerprint: api.WorkFingerprint(in)}
		if r, e := reopened.ReadWorkOperation(t.Context(), q); e != nil || r.Outcome != "accepted" {
			t.Fatal("original offline receipt lost", r, e)
		}
	}
	// Scope is part of the frozen binding used to fence delayed native observations.
	a := worker{Task: api.Task{ID: "task"}, NativeScope: "one"}
	b := a
	b.NativeScope = "two"
	if sameWorkerOwner(a, b) {
		t.Fatal("changed source scope retained owner equality")
	}
}
func TestLegacyForeignBindingRemainsReadOnlyWithoutChangingOriginalReceipts(t *testing.T) {
	rt := &auditSourceTransport{creates: map[string]string{}}
	owner, e := NewWorkOwner(Options{Directory: filepath.Join(t.TempDir(), "private"), WorkSourceIdentity: "original-source"})
	if e != nil {
		t.Fatal(e)
	}
	s := owner.engine
	s.client = &client{origin: "http://127.0.0.1:12345", http: &http.Client{Transport: rt}}
	s.connected = true
	in := api.WorkStart{ID: "old-task", TaskStart: api.TaskStart{RequestID: "old-request", Title: "old", Prompt: "old"}}
	intent := workIntent{in.ID, in.RequestID, "start", api.WorkFingerprint(in)}
	w := worker{Native: true, Task: api.Task{ID: in.ID}, HostStart: &intent, PromptID: (worker{}).operationID("prompt", in.RequestID), Start: &workerStart{Profile: wire.SessionProfile{Model: "controlled"}, Prompt: "old"}}
	s.state.Workers[in.ID] = w
	s.state.Operations[w.PromptID] = journal{Work: &intent, Outcome: "unknown"}
	if _, e = s.advanceWorker(t.Context(), w); e == nil || len(rt.ids) != 0 {
		t.Fatal("legacy foreign work auto-dispatched", rt.ids, e)
	}
	// Known old receipts stay available under exactly the old key.
	s.state.Operations[w.PromptID] = journal{Work: &intent, Outcome: "accepted"}
	q := api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "start", Fingerprint: intent.Fingerprint}
	if r, e := s.ReadWorkOperation(t.Context(), q); e != nil || r.Outcome != "accepted" {
		t.Fatal(r, e)
	}
	if len(rt.ids) != 0 {
		t.Fatal("read replayed native effect", rt.ids)
	}
	s.workSourceIdentity = "" // local/legacy helper contract keeps its original derivation
	if e = s.workerWriteAllowed(w); e != nil || w.operationID("prompt", in.RequestID) != "work-prompt-"+digest([]byte(in.RequestID)) {
		t.Fatal("local/helper derivation changed", e)
	}
}
