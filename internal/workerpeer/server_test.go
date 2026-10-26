package workerpeer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// This fixture is an execution adapter, not a product task manager. It allows
// transport tests to assert isolation without accounts, CLI downloads or models.
type fixtureOwner struct {
	mu            sync.Mutex
	tasks         map[string]api.WorkState
	ops           map[string]api.WorkOperation
	decisions     []api.WorkDecision
	starts, sends int
	file          []byte
	unavailable   bool
}

func newFixtureOwner() *fixtureOwner {
	return &fixtureOwner{tasks: map[string]api.WorkState{}, ops: map[string]api.WorkOperation{}, file: []byte("fixture output")}
}
func (f *fixtureOwner) Connect(context.Context) error {
	if f.unavailable {
		return errors.New("original_owner_unavailable")
	}
	return nil
}
func (f *fixtureOwner) Close(context.Context) error         { return nil }
func (f *fixtureOwner) WorkAdmission(context.Context) error { return nil }
func (*fixtureOwner) ValidateWorkFiles(ctx context.Context, _ string, files []api.InputFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := api.WorkInputDigests(files)
	return err
}
func (f *fixtureOwner) WorkStates() []api.WorkState {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []api.WorkState
	for _, s := range f.tasks {
		out = append(out, s)
	}
	return out
}
func (f *fixtureOwner) StartWork(_ context.Context, in api.WorkStart) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if old, ok := f.tasks[in.ID]; ok {
		return old.Task, nil
	}
	f.starts++
	task := api.Task{ID: in.ID, Title: in.Title, Workspace: in.Workspace, Status: "working", Outcome: "accepted"}
	f.tasks[in.ID] = api.WorkState{Runtime: "codex", Task: task, ExecutionKey: "turn1", StopExecutionKey: "owner-thread-turn1", Activity: "active", AsyncQuestions: []api.Item{{ID: "native-question", AsyncCallID: "original-call", AsyncQuestions: []api.AsyncQuestion{{Title: "fixture question"}}}}}
	q := api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "start", Fingerprint: api.WorkFingerprint(in)}
	f.ops[in.RequestID] = api.WorkOperation{WorkOperationQuery: q, Outcome: "accepted"}
	if len(in.Files) > 0 {
		data, err := os.ReadFile(in.Files[0].Path)
		if err != nil {
			return api.Task{}, err
		}
		f.file = data
	}
	return task, nil
}
func (f *fixtureOwner) ReadWork(_ context.Context, id string) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.tasks[id]
	if !ok {
		return api.Task{}, errors.New("worker_unknown")
	}
	return s.Task, nil
}
func (f *fixtureOwner) SendWork(_ context.Context, in api.TaskMessage) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if q, ok := f.ops[in.RequestID]; ok {
		if q.Fingerprint != api.WorkFingerprint(in) {
			return api.Task{}, errors.New("intent_conflict")
		}
		return f.tasks[in.ID].Task, nil
	}
	s := f.tasks[in.ID]
	s.ExecutionKey = "turn2"
	s.StopExecutionKey = "owner-thread-turn2"
	f.tasks[in.ID] = s
	f.sends++
	q := api.WorkOperationQuery{TaskID: in.ID, RequestID: in.RequestID, Kind: "send", Fingerprint: api.WorkFingerprint(in)}
	f.ops[in.RequestID] = api.WorkOperation{WorkOperationQuery: q, Outcome: "accepted"}
	return s.Task, nil
}
func (f *fixtureOwner) StopWork(context.Context, string) (api.Task, error) {
	return api.Task{}, errors.New("unguarded_stop_forbidden")
}
func (f *fixtureOwner) StopWorkRequest(_ context.Context, in api.WorkStop) (api.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.tasks[in.ID]
	if in.ExecutionKey != s.StopExecutionKey {
		return s.Task, errors.New("stale_execution")
	}
	s.Task.Status = "interrupted"
	f.tasks[in.ID] = s
	return s.Task, nil
}
func (f *fixtureOwner) WorkCapabilities(context.Context, string) (api.WorkCapabilities, error) {
	return api.WorkCapabilities{Contract: api.WorkerContract, Owner: "original-native-owner", Build: "fixture-native-1", Runtime: "codex", History: true, Observe: true, Interactions: true, Files: true, InputMaxFiles: 8, InputMaxFileBytes: api.MaxWorkFileBytes, Reconcile: true, URL: true}, nil
}
func (f *fixtureOwner) ObserveWork(ctx context.Context, id string) (api.WorkObservation, error) {
	task, err := f.ReadWork(ctx, id)
	if err != nil {
		return api.WorkObservation{}, err
	}
	caps, _ := f.WorkCapabilities(ctx, id)
	f.mu.Lock()
	state := f.tasks[id]
	f.mu.Unlock()
	return api.WorkObservation{State: state, Owner: caps, Revision: state.ExecutionKey, Items: []api.Item{{ID: "native-live", Kind: "assistant", Text: "partial", Status: "in_progress"}}, Approvals: []api.Approval{{ID: "native-approval", NativeID: "raw-native-id", Owner: "task", TaskID: id, Details: "opaque details", Status: "pending", Questions: []api.Question{{ID: "secret", Secret: true}}, URL: "http://127.0.0.1/challenge", TaskTitle: task.Title}}}, nil
}
func (f *fixtureOwner) DecideWork(_ context.Context, in api.WorkDecision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tasks[in.TaskID]; !ok {
		return errors.New("worker_not_owned")
	}
	f.decisions = append(f.decisions, in)
	return nil
}
func (f *fixtureOwner) WorkApprovalURL(context.Context, string, string) (string, error) {
	return "http://127.0.0.1/challenge", nil
}
func (f *fixtureOwner) ReadWorkOperation(_ context.Context, q api.WorkOperationQuery) (api.WorkOperation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.ops[q.RequestID]
	if !ok {
		return api.WorkOperation{WorkOperationQuery: q, Outcome: "unknown"}, nil
	}
	if v.TaskID != q.TaskID || v.Kind != q.Kind || v.Fingerprint != q.Fingerprint {
		return api.WorkOperation{}, errors.New("intent_conflict")
	}
	return v, nil
}
func (f *fixtureOwner) ReadWorkConversation(_ context.Context, id string) ([]api.TaskConversationMessage, error) {
	return []api.TaskConversationMessage{{ID: "original-message", Role: "assistant", Text: "completed output", Artifacts: []api.Artifact{{ID: "output", Name: "result.txt"}}}}, nil
}
func (f *fixtureOwner) ReadWorkFile(_ context.Context, id, ref string) (api.WorkFileContent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tasks[id]; !ok || ref != "output" {
		return api.WorkFileContent{}, errors.New("file_not_owned")
	}
	return api.WorkFileContent{Name: "result.txt", Data: f.file}, nil
}
func (f *fixtureOwner) ReceiveQuestionClosure(_ context.Context, c api.QuestionClosure) error {
	if c.ItemID != "native-question" || c.CallID != "original-call" {
		return errors.New("wrong_native_question")
	}
	return nil
}
func peerFixture(t *testing.T) (*Server, *httptest.Server, map[string]*fixtureOwner) {
	t.Helper()
	owners := map[string]*fixtureOwner{}
	var mu sync.Mutex
	server, err := Open(t.TempDir(), func(_ context.Context, source, runtime, root string) (Owner, string, *api.WorkExecutionSettings, error) {
		mu.Lock()
		defer mu.Unlock()
		if owners[source] == nil {
			owners[source] = newFixtureOwner()
		}
		return owners[source], "codex", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	return server, httpServer, owners
}
func pairFixture(t *testing.T, s *Server, h *httptest.Server, source string) Connection {
	t.Helper()
	proof := rand.Text() + rand.Text()
	id := NewPairingID()
	r, err := RequestPairing(t.Context(), h.URL, id, source, "Fixture", proof, s.Status().Peer)
	if err != nil || r.Status != "pending" {
		t.Fatal(r, err)
	}
	if err = s.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: id, Allow: true}); err != nil {
		t.Fatal(err)
	}
	r, err = ReadPairing(t.Context(), h.URL, id, source, proof)
	if err != nil || r.Status != "approved" {
		t.Fatal(r, err)
	}
	return Connection{Address: h.URL, Peer: r.Peer, Token: proof}
}
func TestPeerAuthenticationProjectionIsolationAndLifecycle(t *testing.T) {
	s, h, owners := peerFixture(t)
	a := pairFixture(t, s, h, "source-a")
	c := pairFixture(t, s, h, "source-c")
	for _, path := range []string{Prefix + "call", Prefix + "events?id=task-1"} {
		response, err := http.Get(h.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatalf("loopback trusted: %d", response.StatusCode)
		}
	}
	start := api.WorkStart{TaskStart: api.TaskStart{RequestID: "original-create", Title: "fixture", Prompt: "fixture assignment"}, ID: "task-1", Workspace: filepath.Join(t.TempDir(), "work")}
	for _, connection := range []Connection{a, c} {
		if _, err := Call(t.Context(), connection, Request{Action: "start", Start: start}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Call(t.Context(), a, Request{Action: "observe", ID: start.ID})
	if err != nil {
		t.Fatal(err)
	}
	v := DecodeObservation(out.Observation)
	if v.State.AsyncQuestions[0].AsyncCallID != "original-call" || len(v.State.AsyncQuestions[0].AsyncQuestions) != 1 || v.Approvals[0].TaskID != start.ID || v.Approvals[0].NativeID != "raw-native-id" || !v.Approvals[0].Questions[0].Secret || v.Approvals[0].Details != "opaque details" || v.Items[0].Text != "partial" {
		t.Fatalf("host-only data lost: %#v", v)
	}
	renderer, _ := json.Marshal(v.State)
	if stringContains(string(renderer), "original-call") {
		t.Fatal("renderer question protection removed")
	}
	if _, err = Call(t.Context(), a, Request{Action: "decision", Decision: api.WorkDecision{TaskID: start.ID, RequestID: "original-decision", Decision: api.Decision{ID: "native-approval", Choice: "accept", Answers: map[string][]string{"secret": {"fixture input"}}}}}); err != nil {
		t.Fatal(err)
	}
	if len(owners["source-a"].decisions) != 1 || len(owners["source-c"].decisions) != 0 {
		t.Fatal("same native ID crossed authenticated sources")
	}
	if _, err = Call(t.Context(), c, Request{Action: "read", ID: "task-other"}); err == nil {
		t.Fatal("cross-source read admitted")
	}
	if _, err = Call(t.Context(), a, Request{Action: "model"}); err == nil {
		t.Fatal("helper management command exposed")
	}
	if err = s.Revoke("source-a"); err != nil {
		t.Fatal(err)
	}
	if _, err = Call(t.Context(), a, Request{Action: "read", ID: start.ID}); err == nil {
		t.Fatal("revoked source authenticated")
	}
	if _, err = Call(t.Context(), c, Request{Action: "read", ID: start.ID}); err != nil {
		t.Fatal("unrelated source revoked")
	}
}
func stringContains(a, b string) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i:i+len(b)] == b {
			return true
		}
	}
	return false
}

func TestPairingCannotClaimExistingSourceAndRevocationIsExplicit(t *testing.T) {
	s, h, _ := peerFixture(t)
	a := pairFixture(t, s, h, "source-a")
	if _, err := RequestPairing(t.Context(), h.URL, rand.Text(), "source-a", "Other", rand.Text()+rand.Text(), s.Status().Peer); err == nil {
		t.Fatal("source transferred without original proof")
	}
	if err := s.Revoke("source-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := Call(t.Context(), a, Request{Action: "info"}); err == nil {
		t.Fatal("revoked grant still active")
	}
	id := NewPairingID()
	r, err := RequestPairing(t.Context(), h.URL, id, "source-a", "Fixture", a.Token, s.Status().Peer)
	if err != nil || r.Status != "pending" {
		t.Fatal(r, err)
	}
	if _, err = Call(t.Context(), a, Request{Action: "info"}); err == nil {
		t.Fatal("request reactivated grant before approval")
	}
	if err = s.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: id, Allow: true}); err != nil {
		t.Fatal(err)
	}
}
func TestUnknownOperationReadsOriginalReceiptAndScopedFiles(t *testing.T) {
	s, h, owners := peerFixture(t)
	a := pairFixture(t, s, h, "source-a")
	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte("fixture input"), 0600); err != nil {
		t.Fatal(err)
	}
	start := api.WorkStart{ID: "task-input", Workspace: t.TempDir(), TaskStart: api.TaskStart{RequestID: "original-create", Prompt: "fixture", Files: []api.InputFile{{Name: "input.txt", Path: path}}}}
	if _, err := Call(t.Context(), a, Request{Action: "start", Start: start}); err != nil {
		t.Fatal(err)
	}
	start.FileDigests, _ = api.WorkInputDigests(start.Files)
	q := api.WorkOperationQuery{TaskID: start.ID, RequestID: start.RequestID, Kind: "start", Fingerprint: api.WorkFingerprint(start)}
	out, err := Call(t.Context(), a, Request{Action: "operation", Query: q})
	if err != nil || out.Operation.Outcome != "accepted" {
		t.Fatal(out.Operation, err)
	}
	q.Fingerprint = "different-intent"
	if _, err = Call(t.Context(), a, Request{Action: "operation", Query: q}); err == nil {
		t.Fatal("different intent queried original receipt")
	}
	q.RequestID = "never-dispatched"
	out, err = Call(t.Context(), a, Request{Action: "operation", Query: q})
	if err != nil || out.Operation.Outcome != "unknown" || owners["source-a"].starts != 1 {
		t.Fatal("unknown receipt replayed native execution")
	}
	file, err := Call(t.Context(), a, Request{Action: "file", ID: start.ID, Ref: "output"})
	if err != nil || string(file.File.Data) != "fixture input" {
		t.Fatal("input/output byte path failed", err)
	}
	if _, err = Call(t.Context(), a, Request{Action: "file", ID: start.ID, Ref: path}); err == nil {
		t.Fatal("arbitrary file path accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	count := 0
	err = Watch(ctx, a, start.ID, func(v api.WorkObservation) { count++; cancel() })
	if count != 1 {
		t.Fatal("bounded Worker SSE did not project original snapshot", err)
	}
	req, _ := http.NewRequest("POST", h.URL+Prefix+"call", nil)
	req.Header.Set("Origin", "http://localhost:29317")
	req.Header.Set("Authorization", "Bearer "+a.Token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("browser Origin inherited peer authority")
	}
}

func TestPeerCarriesFailureAndOriginalStopFacts(t *testing.T) {
	server, httpServer, owners := peerFixture(t)
	connection := pairFixture(t, server, httpServer, "failure-source")
	start := api.WorkStart{TaskStart: api.TaskStart{RequestID: "original-create", Title: "fixture", Prompt: "fixture"}, ID: "failure-task", Workspace: filepath.Join(t.TempDir(), "work")}
	if _, err := Call(t.Context(), connection, Request{Action: "start", Start: start}); err != nil {
		t.Fatal(err)
	}
	owner := owners["failure-source"]
	owner.mu.Lock()
	state := owner.tasks[start.ID]
	state.Task.Status = "failed"
	state.Task.Failure = api.UnknownTaskFailure("caelis", "original-native-turn")
	state.StopAcknowledged = true
	owner.tasks[start.ID] = state
	owner.mu.Unlock()
	out, err := Call(t.Context(), connection, Request{Action: "observe", ID: start.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := DecodeObservation(out.Observation)
	if got.State.Task.Failure == nil || got.State.Task.Failure.Execution != "original-native-turn" || got.State.Task.Failure.Retryable != nil || !got.State.StopAcknowledged {
		t.Fatal("peer lost native failure/stop facts", got)
	}
	read, err := Call(t.Context(), connection, Request{Action: "read", ID: start.ID})
	if err != nil || read.Task.Failure == nil || read.Task.Failure.Code != "unknown" {
		t.Fatal("peer read lost failure", read, err)
	}
}
