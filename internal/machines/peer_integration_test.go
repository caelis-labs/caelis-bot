package machines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

// These fixtures model only the native execution boundary. Both product task
// coordination and the authenticated HTTP wire are the production code.
type dualPeerLocal struct {
	inert
	mutations atomic.Int32
}

func (*dualPeerLocal) BindWork(context.Context, api.TaskStart, string, string) (string, error) {
	return "codex", nil
}
func (*dualPeerLocal) OwnsWork(runtime string) bool { return runtime == "codex" || runtime == "caelis" }
func (f *dualPeerLocal) StartWork(context.Context, api.WorkStart) (api.Task, error) {
	f.mutations.Add(1)
	return api.Task{}, errors.New("unexpected_local_execution")
}
func (f *dualPeerLocal) SendWork(context.Context, api.TaskMessage) (api.Task, error) {
	f.mutations.Add(1)
	return api.Task{}, errors.New("unexpected_local_continuation")
}

type dualPeerNative struct {
	mu                   sync.Mutex
	source, runtime      string
	states               map[string]api.WorkState
	receipts             map[string]api.WorkOperation
	starts, sends, stops int
	decisions            []api.WorkDecision
	closures             []api.QuestionClosure
	input                map[string][]byte
}

func newDualNative(source, runtime string) *dualPeerNative {
	return &dualPeerNative{source: source, runtime: runtime, states: map[string]api.WorkState{}, receipts: map[string]api.WorkOperation{}, input: map[string][]byte{}}
}
func (*dualPeerNative) Connect(context.Context) error       { return nil }
func (*dualPeerNative) Close(context.Context) error         { return nil }
func (*dualPeerNative) WorkAdmission(context.Context) error { return nil }
func (f *dualPeerNative) WorkStates() []api.WorkState {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []api.WorkState{}
	for _, v := range f.states {
		out = append(out, v)
	}
	return out
}
func (f *dualPeerNative) StartWork(_ context.Context, in api.WorkStart) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if old, ok := f.states[in.ID]; ok {
		return old.Task, nil
	}
	if in.Machine != "" || in.ExecutionOverride == nil || in.ExecutionOverride.Model != "B-"+f.runtime {
		return api.Task{}, errors.New("caller_managed_target_configuration")
	}
	task := api.Task{ID: in.ID, Title: in.Title, Workspace: in.Workspace, Status: "working", Outcome: "accepted"}
	f.starts++
	f.states[in.ID] = api.WorkState{Runtime: f.runtime, Task: task, ExecutionKey: "turn-one", StopExecutionKey: "original-owner-turn-one", Activity: "active", TurnKey: "turn-one", AsyncQuestions: []api.Item{{ID: "same-native-question", AsyncCallID: "native-call-original", AsyncQuestions: []api.AsyncQuestion{{Title: "Choose native answer", Options: []string{"one", "two"}}}}}}
	q := api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "start", Fingerprint: api.WorkFingerprint(in)}
	f.receipts[in.RequestID] = api.WorkOperation{WorkOperationQuery: q, Outcome: "accepted"}
	if len(in.Files) > 0 {
		data, err := os.ReadFile(in.Files[0].Path)
		if err != nil {
			return task, err
		}
		f.input[in.ID] = data
	}
	return task, nil
}
func (f *dualPeerNative) ReadWork(_ context.Context, id string) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.states[id]
	if !ok {
		return api.Task{}, errors.New("native_task_not_owned")
	}
	return v.Task, nil
}
func (f *dualPeerNative) SendWork(_ context.Context, in api.TaskMessage) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.states[in.ID]
	if !ok {
		return api.Task{}, errors.New("native_task_not_owned")
	}
	if old, ok := f.receipts[in.RequestID]; ok {
		if old.Fingerprint != api.WorkFingerprint(in) {
			return v.Task, errors.New("original_intent_conflict")
		}
		return v.Task, nil
	}
	f.sends++
	v.ExecutionKey = "turn-two"
	v.StopExecutionKey = "original-owner-turn-two"
	v.TurnKey = "turn-two"
	f.states[in.ID] = v
	q := api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "send", Fingerprint: api.WorkFingerprint(in)}
	f.receipts[in.RequestID] = api.WorkOperation{WorkOperationQuery: q, Outcome: "accepted"}
	return v.Task, nil
}
func (*dualPeerNative) StopWork(context.Context, string) (api.Task, error) {
	return api.Task{}, errors.New("unguarded_native_stop_forbidden")
}
func (f *dualPeerNative) StopWorkRequest(_ context.Context, in api.WorkStop) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.states[in.ID]
	if !ok || in.ExecutionKey != v.StopExecutionKey {
		return v.Task, errors.New("original_execution_changed")
	}
	f.stops++
	v.Task.Status = "interrupted"
	f.states[in.ID] = v
	return v.Task, nil
}
func (f *dualPeerNative) WorkCapabilities(_ context.Context, id string) (api.WorkCapabilities, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != "" {
		if _, ok := f.states[id]; !ok {
			return api.WorkCapabilities{}, errors.New("native_task_not_owned")
		}
	}
	return api.WorkCapabilities{Contract: api.WorkerContract, Owner: f.source + ":" + f.runtime, Build: "actual-native-fixture", Runtime: f.runtime, History: true, Observe: true, Interactions: true, Files: true, Reconcile: true, URL: true}, nil
}
func (f *dualPeerNative) ObserveWork(ctx context.Context, id string) (api.WorkObservation, error) {
	caps, err := f.WorkCapabilities(ctx, id)
	if err != nil {
		return api.WorkObservation{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.states[id]
	return api.WorkObservation{State: v, Owner: caps, Revision: v.ExecutionKey, Items: []api.Item{{ID: "same-native-live", Kind: "assistant", Text: "native partial output", Status: "inProgress", AsyncCallID: "hidden-live-call", AsyncQuestions: []api.AsyncQuestion{{Title: "hidden live question"}}, Artifacts: []api.Artifact{{ID: "output:" + id, Name: "result.txt"}}}}, Approvals: []api.Approval{{ID: "same-native-approval", NativeID: "raw-native-request", Owner: "task", TaskID: id, TaskTitle: v.Task.Title, Status: "pending", Details: "full opaque native details", Questions: []api.Question{{ID: "sensitive-native-field", Secret: true}}, URL: "http://127.0.0.1/native/challenge"}}}, nil
}
func (f *dualPeerNative) DecideWork(_ context.Context, in api.WorkDecision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.states[in.TaskID]; !ok || in.Decision.ID != "same-native-approval" || in.RequestID == "" {
		return errors.New("original_native_request_required")
	}
	f.decisions = append(f.decisions, in)
	return nil
}
func (f *dualPeerNative) WorkApprovalURL(ctx context.Context, id, ref string) (string, error) {
	if _, err := f.ReadWork(ctx, id); err != nil || ref != "same-native-approval" {
		return "", errors.New("native_request_not_owned")
	}
	return "http://127.0.0.1/native/challenge", nil
}
func (f *dualPeerNative) ReceiveQuestionClosure(_ context.Context, in api.QuestionClosure) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.states[in.WorkID]; !ok || in.ItemID != "same-native-question" || in.CallID != "native-call-original" {
		return errors.New("native_question_scope_conflict")
	}
	f.closures = append(f.closures, in)
	return nil
}
func (f *dualPeerNative) ReadWorkOperation(_ context.Context, q api.WorkOperationQuery) (api.WorkOperation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.receipts[q.RequestID]
	if !ok {
		return api.WorkOperation{WorkOperationQuery: q, Outcome: "unknown"}, nil
	}
	if v.WorkOperationQuery != q {
		return api.WorkOperation{}, errors.New("original_operation_digest_or_owner_conflict")
	}
	return v, nil
}
func (f *dualPeerNative) ReadWorkConversation(ctx context.Context, id string) ([]api.TaskConversationMessage, error) {
	if _, err := f.ReadWork(ctx, id); err != nil {
		return nil, err
	}
	return []api.TaskConversationMessage{{ID: "same-native-message", Role: "assistant", Text: "native history", Artifacts: []api.Artifact{{ID: "output:" + id, Name: "result.txt"}}}}, nil
}
func (f *dualPeerNative) ReadWorkFile(ctx context.Context, id, ref string) (api.WorkFileContent, error) {
	if _, err := f.ReadWork(ctx, id); err != nil || ref != "output:"+id {
		return api.WorkFileContent{}, errors.New("native_file_not_owned")
	}
	return api.WorkFileContent{Name: "result.txt", Data: []byte("result for " + id)}, nil
}

type dualPeerTarget struct {
	mu                 sync.Mutex
	defaultRuntime     string
	owners             map[string]*dualPeerNative
	peer               *workerpeer.Server
	http               *httptest.Server
	offline, dropStart atomic.Bool
}

func newDualPeerTarget(t *testing.T) *dualPeerTarget {
	t.Helper()
	f := &dualPeerTarget{defaultRuntime: "codex", owners: map[string]*dualPeerNative{}}
	peer, err := workerpeer.Open(t.TempDir(), func(_ context.Context, source, runtime, _ string) (workerpeer.Owner, string, *api.WorkExecutionSettings, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if runtime == "" {
			runtime = f.defaultRuntime
		}
		key := source + "\x00" + runtime
		owner := f.owners[key]
		if owner == nil {
			owner = newDualNative(source, runtime)
			f.owners[key] = owner
		}
		return owner, runtime, &api.WorkExecutionSettings{Model: "B-" + runtime}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.peer = peer
	if err = peer.Enable(true); err != nil {
		t.Fatal(err)
	}
	f.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.offline.Load() {
			http.Error(w, "isolated fixture disconnected", 503)
			return
		}
		if r.URL.Path == workerpeer.Prefix+"call" && f.dropStart.Load() {
			b, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(b))
			var req workerpeer.Request
			_ = json.Unmarshal(b, &req)
			if req.Action == "start" && f.dropStart.CompareAndSwap(true, false) {
				rec := httptest.NewRecorder()
				peer.ServeHTTP(rec, r)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
				return
			}
		}
		peer.ServeHTTP(w, r)
	}))
	t.Cleanup(f.http.Close)
	t.Cleanup(func() { _ = peer.Close(context.Background()) })
	return f
}
func (f *dualPeerTarget) setDefault(runtime string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultRuntime = runtime
}
func (f *dualPeerTarget) owner(source, runtime string) *dualPeerNative {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.owners[source+"\x00"+runtime]
}
func dualPeerConnect(t *testing.T, target *dualPeerTarget) (*Service, *tasks.Manager, *dualPeerLocal, string, string) {
	t.Helper()
	local := &dualPeerLocal{}
	root := t.TempDir()
	service, err := Open(filepath.Join(root, "machines"), local, nil)
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := target.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	machine, err := service.ConnectBot(t.Context(), api.BotConnectionInput{ID: "bot-fixture-target", Name: "Machine B", Address: target.http.URL, Code: pairing.Code})
	if err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(root, "tasks.json")
	manager, err := tasks.Open(ledger, filepath.Join(root, "Tasks"), "codex", service, reviewReports{}, func() api.Snapshot { return api.Snapshot{} })
	if err != nil {
		t.Fatal(err)
	}
	return service, manager, local, machine.ID, ledger
}

func dualPeerSnapshot(t *testing.T, connection workerpeer.Connection, id string) api.WorkObservation {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	snapshots := make(chan api.WorkObservation, 1)
	done := make(chan error, 1)
	go func() {
		done <- workerpeer.Watch(ctx, connection, id, func(v api.WorkObservation) {
			select {
			case snapshots <- v:
			default:
			}
		})
	}()
	select {
	case snapshot := <-snapshots:
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Worker SSE cancellation did not finish")
		}
		return snapshot
	case <-ctx.Done():
		t.Fatal("Worker SSE did not publish a current native snapshot")
		return api.WorkObservation{}
	}
}

func TestBotPeerDualInstanceProductAuthorityAndFrozenOwner(t *testing.T) {
	target := newDualPeerTarget(t)
	service, manager, local, machine, ledger := dualPeerConnect(t, target)
	// A's machine card still says Codex, but B's own setting is authoritative at
	// preparation and the Manager must persist the selected native owner.
	target.setDefault("caelis")
	file := filepath.Join(t.TempDir(), "input.txt")
	_ = os.WriteFile(file, []byte("task-scoped input"), 0600)
	first, err := manager.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "stable-first", Title: "First", Prompt: "work", Files: []api.InputFile{{Name: "input.txt", Path: file}}})
	if err != nil {
		t.Fatal("prepare/start did not honor B's actual default", err)
	}
	if service.state.Runtimes[first.ID] != "caelis" {
		t.Fatal("stale A runtime was persisted", service.state.Runtimes[first.ID])
	}
	owner := target.owner(service.state.ProfileID, "caelis")
	if owner == nil {
		t.Fatal("B isolated original owner missing")
	}
	owner.mu.Lock()
	input := string(owner.input[first.ID])
	owner.mu.Unlock()
	if input != "task-scoped input" {
		t.Fatal("worker input did not cross task-scoped upload")
	}
	second, err := manager.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "stable-second", Title: "Second", Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	one, err := manager.ObserveTask(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	two, err := manager.ObserveTask(t.Context(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(one.State.AsyncQuestions) != 1 || one.State.AsyncQuestions[0].AsyncCallID != "native-call-original" || len(one.State.AsyncQuestions[0].AsyncQuestions) != 1 || len(one.Items) != 1 || one.Items[0].AsyncCallID != "hidden-live-call" || len(one.Items[0].AsyncQuestions) != 1 {
		t.Fatal("host-only native interaction fields lost in transport")
	}
	if len(one.Approvals) != 1 || one.Approvals[0].TaskID != first.ID || one.Approvals[0].NativeID != "raw-native-request" || one.Approvals[0].Details != "full opaque native details" || !one.Approvals[0].Questions[0].Secret || one.Approvals[0].URL == "" || one.Approvals[0].ID == two.Approvals[0].ID {
		t.Fatal("full approvals lost task/native scope")
	}
	if one.State.AsyncQuestions[0].ID == two.State.AsyncQuestions[0].ID {
		t.Fatal("identical native question IDs collided across tasks")
	}
	retained, _ := service.RetainedApprovalState()
	if len(retained) != 2 {
		t.Fatal("A retained approval path omitted remote requests", len(retained))
	}
	handled, err := service.DecideRetainedApproval(t.Context(), api.Decision{ID: one.Approvals[0].ID, Answers: map[string][]string{"sensitive-native-field": {"fixture-only-secret"}}})
	if !handled || err != nil {
		t.Fatal("A retained decision did not reach original B owner", handled, err)
	}
	owner.mu.Lock()
	decisions := append([]api.WorkDecision(nil), owner.decisions...)
	owner.mu.Unlock()
	if len(decisions) != 1 || decisions[0].TaskID != first.ID || decisions[0].Decision.ID != "same-native-approval" || decisions[0].RequestID == "" || decisions[0].Decision.Answers["sensitive-native-field"][0] != "fixture-only-secret" {
		t.Fatal("decision lost original native identity")
	}
	if err = service.ReceiveQuestionClosure(t.Context(), api.QuestionClosure{ID: "closure", WorkID: first.ID, ItemID: one.State.AsyncQuestions[0].ID, CallID: "native-call-original", State: "closed"}); err != nil {
		t.Fatal("question closure lost native ID", err)
	}
	history, err := manager.ReadTaskConversation(t.Context(), first.ID)
	if err != nil || len(history.Messages) != 1 || len(history.Messages[0].Artifacts) != 1 {
		t.Fatal("original native history unreadable", err)
	}
	outputRef := one.Items[0].Artifacts[0].ID
	output, err := manager.ReadTaskFile(t.Context(), first.ID, outputRef)
	if err != nil || string(output.Data) != "result for "+first.ID {
		t.Fatal("original worker file unreadable", err)
	}
	if _, err = manager.ReadTaskFile(t.Context(), second.ID, outputRef); err == nil {
		t.Fatal("another task could read source file")
	}
	oldStop := api.WorkStop{ID: first.ID, RequestID: "old-stop", ExecutionKey: one.State.StopExecutionKey}
	target.setDefault("codex")
	if _, err = manager.SendTask(t.Context(), api.TaskMessage{ID: first.ID, RequestID: "continue-original", Prompt: "continue"}); err != nil {
		t.Fatal("existing task changed owner after B default switched", err)
	}
	if service.state.Runtimes[first.ID] != "caelis" {
		t.Fatal("continuation rebound runtime")
	}
	if _, err = service.StopWorkRequest(t.Context(), oldStop); err == nil {
		t.Fatal("late original stop interrupted later turn")
	}
	owner.mu.Lock()
	sends, stops := owner.sends, owner.stops
	owner.mu.Unlock()
	if sends != 1 || stops != 0 {
		t.Fatal("original owner generation was not fenced", sends, stops)
	}
	connection, _, err := service.connection(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	streamed := dualPeerSnapshot(t, connection, first.ID)
	reconnected := dualPeerSnapshot(t, connection, first.ID)
	if streamed.State.ExecutionKey != "turn-two" || reconnected.Owner.Owner != streamed.Owner.Owner || len(reconnected.State.AsyncQuestions) != 1 || reconnected.State.AsyncQuestions[0].AsyncCallID != "native-call-original" || reconnected.Approvals[0].NativeID != "raw-native-request" {
		t.Fatal("SSE reconnect replaced the original owner or lost cross-turn native pending requests")
	}
	// B product Manager sees only B's ordinary local source, never source owners
	// allocated by the authenticated execution endpoint.
	bLocal := &dualPeerLocal{}
	bRoot := t.TempDir()
	bManager, err := tasks.Open(filepath.Join(bRoot, "tasks.json"), filepath.Join(bRoot, "Tasks"), "codex", bLocal, reviewReports{}, func() api.Snapshot { return api.Snapshot{} })
	if err != nil || len(bManager.ListTasks()) != 0 {
		t.Fatal("B imported foreign worker into product task list", err)
	}
	target.offline.Store(true)
	if _, err = manager.SendTask(t.Context(), api.TaskMessage{ID: first.ID, RequestID: "offline-original", Prompt: "continue"}); err == nil {
		t.Fatal("disconnection fabricated remote success")
	}
	if local.mutations.Load() != 0 {
		t.Fatal("disconnected remote worker fell back to local")
	}
	target.offline.Store(false)
	reopened, err := Open(service.root, local, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := tasks.Open(ledger, filepath.Join(filepath.Dir(ledger), "Tasks"), "codex", reopened, reviewReports{}, func() api.Snapshot { return api.Snapshot{} })
	if err != nil {
		t.Fatal("A reopen lost frozen route", err)
	}
	if _, err = restored.ReadTask(t.Context(), first.ID); err != nil || reopened.state.Runtimes[first.ID] != "caelis" || reopened.state.Routes[first.ID] != machine {
		t.Fatal("reopen adopted another owner", err)
	}
}

func TestBotPeerDualInstanceUnknownStartReconcilesAndSourcesIsolate(t *testing.T) {
	target := newDualPeerTarget(t)
	service, manager, local, machine, _ := dualPeerConnect(t, target)
	target.dropStart.Store(true)
	first, err := manager.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "same-request", Title: "First", Prompt: "work"})
	if err == nil || first.ID == "" {
		t.Fatal("lost HTTP response was not retained as an original task", first.ID, err)
	}
	receipt, err := manager.ReconcileTaskRequest(t.Context(), first.ID, "same-request")
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal("original native operation could not be reconciled", receipt, err)
	}
	again, err := manager.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "same-request", Title: "First", Prompt: "work"})
	if err != nil || again.ID != first.ID {
		t.Fatal("same request was replaced", again.ID, err)
	}
	owner := target.owner(service.state.ProfileID, "codex")
	owner.mu.Lock()
	starts := owner.starts
	owner.mu.Unlock()
	if starts != 1 || local.mutations.Load() != 0 {
		t.Fatal("lost response replayed native creation", starts)
	}
	// A second independently authenticated Bot intentionally uses the same task
	// and native IDs. B must bind it to a separate source owner.
	other, otherManager, _, otherMachine, _ := dualPeerConnect(t, target)
	second, err := otherManager.StartTask(t.Context(), api.TaskStart{Machine: otherMachine, RequestID: "same-request", Title: "Second source", Prompt: "other work"})
	if err != nil || second.ID != first.ID {
		t.Fatal("source-isolation fixture did not exercise equal task IDs", second.ID, first.ID, err)
	}
	otherOwner := target.owner(other.state.ProfileID, "codex")
	if otherOwner == nil || otherOwner == owner {
		t.Fatal("authenticated sources share native owner")
	}
	one, err := manager.ObserveTask(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	two, err := otherManager.ObserveTask(t.Context(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if one.State.Task.Title != "First" || two.State.Task.Title != "Second source" || one.Owner.Owner == two.Owner.Owner {
		t.Fatal("identical native IDs leaked across authenticated sources")
	}
	c, _, err := other.connection(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	exclusive, err := manager.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "only-first-source", Title: "Exclusive", Prompt: "source-bound work"})
	if err != nil {
		t.Fatal(err)
	}
	requests := []workerpeer.Request{
		{Action: "observe", ID: exclusive.ID},
		{Action: "messages", ID: exclusive.ID},
		{Action: "file", ID: exclusive.ID, Ref: "output:" + exclusive.ID},
		{Action: "send", Message: api.TaskMessage{ID: exclusive.ID, RequestID: "foreign-write", Prompt: "must not run"}},
		{Action: "decision", Decision: api.WorkDecision{TaskID: exclusive.ID, RequestID: "foreign-decision", Decision: api.Decision{ID: "same-native-approval", Choice: "allow"}}},
	}
	for _, request := range requests {
		request.Runtime = "codex"
		if _, err = workerpeer.Call(t.Context(), c, request); err == nil || !strings.Contains(err.Error(), "worker_not_owned") {
			t.Fatal("authenticated source crossed another source's task boundary", request.Action, err)
		}
	}
}

func TestBotPeerPairResponseLossRecoversOriginalGrant(t *testing.T) {
	target := newDualPeerTarget(t)
	var drop atomic.Bool
	drop.Store(true)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerpeer.Prefix+"pair" && drop.CompareAndSwap(true, false) {
			rec := httptest.NewRecorder()
			target.peer.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				t.Errorf("pair fixture failed before response loss: %d", rec.Code)
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		target.peer.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	root := t.TempDir()
	service, err := Open(root, &dualPeerLocal{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := target.peer.Pairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	in := api.BotConnectionInput{Name: "original target", Address: proxy.URL, Code: pairing.Code}
	if _, err = service.ConnectBot(t.Context(), in); err == nil {
		t.Fatal("lost pair response became success")
	}
	if len(service.state.Profiles) != 1 {
		t.Fatal("original pending profile was not retained")
	}
	var id, token string
	for key, p := range service.state.Profiles {
		id, token = key, p.Peer.Token
	}
	if token == "" {
		t.Fatal("original credential not persisted before pair")
	}
	// Simulate A restarting. The used code cannot be sent again; only an
	// authenticated read of the already committed original grant is permitted.
	reopened, err := Open(root, &dualPeerLocal{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	in.Code = "already-consumed"
	connected, err := reopened.ConnectBot(t.Context(), in)
	if err != nil {
		t.Fatal("original grant was not reconciled", err)
	}
	if connected.ID != id || reopened.state.Profiles[id].Peer.Token != token || len(target.peer.Status().Sources) != 1 {
		t.Fatal("pair recovery replaced identity, token or source")
	}
}
