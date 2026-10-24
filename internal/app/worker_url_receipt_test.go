package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type retainedURLReceiptOwner struct {
	*workerFixture
	reads int
}

func (o *retainedURLReceiptOwner) Connect(context.Context) error {
	o.connections++
	return errors.New("original_owner_exited")
}
func (o *retainedURLReceiptOwner) ReadWorkURL(context.Context, string, string) (api.WorkURLPage, error) {
	return api.WorkURLPage{}, errors.New("original_request_invalid")
}
func (o *retainedURLReceiptOwner) SubmitWorkURL(context.Context, api.WorkURLSubmission) (api.WorkOperation, error) {
	return api.WorkOperation{}, errors.New("original_request_invalid")
}
func (o *retainedURLReceiptOwner) ReadWorkURLSubmission(_ context.Context, task, id, op string) (api.WorkOperation, error) {
	o.reads++
	if task != "original-task" || id != "original-request" || op != "original-op" {
		return api.WorkOperation{}, errors.New("wrong_reference")
	}
	return api.WorkOperation{WorkOperationQuery: api.WorkOperationQuery{TaskID: task, RequestID: op, Kind: api.WorkURLSubmitKind, Fingerprint: "original-digest"}, Outcome: "unknown"}, nil
}
func (o *retainedURLReceiptOwner) ReadWorkOperation(_ context.Context, in api.WorkOperationQuery) (api.WorkOperation, error) {
	o.reads++
	return api.WorkOperation{WorkOperationQuery: in, Outcome: "accepted"}, nil
}
func TestRetainedOperationReceiptReadDoesNotConnect(t *testing.T) {
	owner := &retainedURLReceiptOwner{workerFixture: &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}}
	w := &localWorkers{active: "caelis", resident: &workerFixture{runtime: "caelis"}, owners: map[string]retainedWorker{"codex": owner}, routes: map[string]string{"original-task": "codex"}}
	in := api.WorkOperationQuery{TaskID: "original-task", RequestID: "original-send", Kind: "send", Fingerprint: "exact-digest"}
	op, err := w.ReadWorkOperation(t.Context(), in)
	if err != nil || op.Outcome != "accepted" || owner.connections != 0 || owner.reads != 1 {
		t.Fatal(op, err, owner.connections, owner.reads)
	}
	in.TaskID = "unbound-task"
	if _, err = w.ReadWorkOperation(t.Context(), in); err == nil || owner.reads != 1 {
		t.Fatal("unbound receipt read", err, owner.reads)
	}
}

func TestRetainedURLReceiptReadDoesNotRequireLiveNativeOwner(t *testing.T) {
	owner := &retainedURLReceiptOwner{workerFixture: &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}}
	w := &localWorkers{active: "caelis", resident: &workerFixture{runtime: "caelis"}, owners: map[string]retainedWorker{"codex": owner}, routes: map[string]string{"original-task": "codex"}}
	op, err := w.ReadWorkURLSubmission(t.Context(), "original-task", "original-request", "original-op")
	if err != nil || op.Outcome != "unknown" || op.Fingerprint != "original-digest" || owner.connections != 0 || owner.reads != 1 {
		t.Fatalf("original receipt waited for dead native owner: %+v %v connects=%d reads=%d", op, err, owner.connections, owner.reads)
	}
	if _, err = w.ReadWorkURLSubmission(t.Context(), "unbound-task", "original-request", "original-op"); err == nil || owner.reads != 1 {
		t.Fatal("unbound task read retained receipt", err, owner.reads)
	}
}

type disconnectedConversationEngine struct {
	*testEngine
	modelReads int
}

func (*disconnectedConversationEngine) ProviderInfo() api.ProviderInfo {
	return api.ProviderInfo{ID: "codex", Name: "Codex"}
}
func (e *disconnectedConversationEngine) ConversationExecution(context.Context) (api.WorkExecutionSettings, error) {
	e.modelReads++
	return api.WorkExecutionSettings{}, errors.New("resident_offline")
}
func TestForeignReceiptOwnerLoadIsIndependentOfResidentReadiness(t *testing.T) {
	engine := &disconnectedConversationEngine{testEngine: newTestEngine()}
	root := t.TempDir()
	if err := localstate.Write(filepath.Join(root, "runtime.json"), api.RuntimeSettings{Runtime: "codex", CLIPath: "/unavailable-original-native"}); err != nil {
		t.Fatal(err)
	}
	a, err := newApplication(root, Host{}, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) { return engine, nil }}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := backend.SaveRuntimeProfile(root, api.RuntimeSettings{Runtime: "codex", CLIPath: "/unavailable-original-native"}); err != nil {
		t.Fatal(err)
	}
	sourceRoot := filepath.Join(root, "WorkerPeers", "sources", "original-source")
	private := filepath.Join(sourceRoot, "codex")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := localstate.Write(filepath.Join(private, "native-endpoint.json"), map[string]any{"endpoint": "unix:///unavailable-original/runtime.sock"}); err != nil {
		t.Fatal(err)
	}
	if err := localstate.Write(filepath.Join(private, "worker-bindings.json"), map[string]any{"version": 1, "tasks": map[string]any{"original-task": map[string]any{"view": api.Task{ID: "original-task"}, "requests": map[string]any{"original-op": map[string]any{"kind": api.WorkURLSubmitKind, "controlTarget": "original-request", "intentFingerprint": "original-digest", "outcome": "unknown"}}}}}); err != nil {
		t.Fatal(err)
	}
	var factoryErr error
	// Exercise the real authenticated handler/context, not a public caller flag.
	data := map[string]any{"version": 1, "identity": "original-peer", "enabled": true, "grants": map[string]any{"original-source": map[string]any{"peer": "original-source", "hash": fmt.Sprintf("%x", sha256.Sum256([]byte("original-proof")))}}, "bindings": map[string]any{"original-source": map[string]string{"original-task": "codex"}}}
	if err := localstate.Write(filepath.Join(root, "receipt-peer", "peers.json"), data); err != nil {
		t.Fatal(err)
	}
	peer, err := workerpeer.Open(filepath.Join(root, "receipt-peer"), func(ctx context.Context, source, runtime, dir string) (workerpeer.Owner, string, *api.WorkExecutionSettings, error) {
		owner, kind, work, e := a.openForeignWorker(ctx, source, runtime, sourceRoot)
		factoryErr = e
		return owner, kind, work, e
	})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(peer)
	defer h.Close()
	conn := workerpeer.Connection{Address: h.URL, Peer: "original-peer", Token: "original-proof"}
	in := workerpeer.Request{Action: "url-operation", ID: "original-task", Ref: "original-request", Query: api.WorkOperationQuery{RequestID: "original-op"}}
	out, err := workerpeer.Call(t.Context(), conn, in)
	if err != nil || out.Operation.Outcome != "unknown" || engine.modelReads != 0 {
		t.Fatal("original receipt depended on resident readiness", out.Operation, err, factoryErr, engine.modelReads)
	}
	general := workerpeer.Request{Action: "operation", Query: api.WorkOperationQuery{TaskID: "original-task", RequestID: "original-op", Kind: api.WorkURLSubmitKind, Fingerprint: "original-digest"}}
	out, err = workerpeer.Call(t.Context(), conn, general)
	if err != nil || out.Operation.Outcome != "unknown" || engine.modelReads != 0 {
		t.Fatal("generic receipt depended on resident readiness", out.Operation, err, factoryErr, engine.modelReads)
	}
	// Readiness/admission still consults B's current model, never this read-only mode.
	if _, _, _, err = a.openForeignWorker(t.Context(), "new-source", "", filepath.Join(root, "new-source")); err == nil || engine.modelReads != 1 {
		t.Fatal("new work skipped actual conversation selection", err, engine.modelReads)
	}
}
