package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type attestedLocalFixture struct {
	*remoteFixture
}

func (f *attestedLocalFixture) WorkMessageRecorded(in api.TaskMessage) bool {
	_, ok := f.requests[in.RequestID]
	return ok
}

func (f *attestedLocalFixture) readIntent() (state, error) {
	b, err := os.ReadFile(f.ledger)
	var saved state
	if err == nil {
		err = json.Unmarshal(b, &saved)
	}
	return saved, err
}

func (f *attestedLocalFixture) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	saved, err := f.readIntent()
	if err != nil {
		return api.Task{}, err
	}
	r := saved.Records[in.ID]
	if r == nil || r.Source != in.Source || r.RequestDigest != in.RequestDigest {
		return api.Task{}, errors.New("native start preceded its durable source binding")
	}
	return f.fixtureRuntime.StartWork(ctx, in)
}

func (f *attestedLocalFixture) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	saved, err := f.readIntent()
	if err != nil {
		return api.Task{}, err
	}
	original, ok := saved.Messages[in.RequestID]
	if !ok || original.Source != in.Source || original.RequestDigest != in.RequestDigest {
		return api.Task{}, errors.New("native continuation preceded its durable source binding")
	}
	return f.remoteFixture.SendWork(ctx, in)
}

func TestRoutedLocalWorkBindsNativeSourceBeforeDispatchAndKeepsOriginalReceipt(t *testing.T) {
	root := t.TempDir()
	f := &attestedLocalFixture{&remoteFixture{fixtureRuntime: newRuntime(), ledger: filepath.Join(root, "tasks.json"), requests: map[string]api.TaskMessage{}}}
	source := &sourceFixture{source: api.WorkDispatchSource{NodeID: api.LocalNodeID, Backend: "codex", Kind: "native_activation", BindingID: "actual-binding", OperationID: "original-activation"}}
	m, err := OpenRouted(f.ledger, filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot, nil, source)
	if err != nil {
		t.Fatal(err)
	}
	f.unknownStart = true
	in := input("local-attested-start")
	v, err := m.StartTask(t.Context(), in)
	if err == nil || v.Outcome != "unknown" || f.lastStart.Source != source.source || len(f.lastStart.RequestDigest) != 64 {
		t.Fatal("unknown start lost its attested intent", v, f.lastStart, err)
	}
	source.source.OperationID = "later-activation"
	if again, err := m.StartTask(t.Context(), in); err != nil || again.ID != v.ID || f.starts != 1 {
		t.Fatal("unknown local start was redispatched", again, err)
	}
	msg := api.TaskMessage{ID: v.ID, RequestID: "local-attested-continue", Prompt: "Continue", Source: api.WorkDispatchSource{Kind: "fabricated"}, RequestDigest: "fabricated"}
	if _, err := m.SendTask(t.Context(), msg); err != nil || f.lastMessage.Source != source.source || len(f.lastMessage.RequestDigest) != 64 {
		t.Fatal("local continuation lost native provenance", f.lastMessage, err)
	}
	original := f.lastMessage
	source.source.OperationID = "new-activation"
	m, err = OpenRouted(m.path, m.root, "codex", f, f, f.Snapshot, nil, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SendTask(t.Context(), msg); err != nil || f.sends != 1 || f.lastMessage != original {
		t.Fatal("reopened continuation changed original intent", f.lastMessage, err)
	}
	msg.Prompt = "A different continuation"
	if _, err := m.SendTask(t.Context(), msg); err == nil || f.sends != 1 {
		t.Fatal("conflicting original request dispatched")
	}
}

func TestRoutedLocalSourceRejectsInvalidAuthorityBeforeWorkspaceOrDispatch(t *testing.T) {
	for _, mode := range []string{"inactive", "invalid", "node", "backend", "lease", "admission"} {
		t.Run(mode, func(t *testing.T) {
			m, local, _, source, _, _ := routedFixture(t)
			switch mode {
			case "inactive":
				source.err = api.ErrWorkSourceInactive
			case "invalid":
				source.source.BindingID = ""
			case "node":
				source.source.NodeID = "other-resident"
			case "backend":
				source.source.Backend = "caelis"
			case "lease":
				source.source.Lease = api.WorkerLeaseGrant{BotID: "bot", BrokerNodeID: "broker", SourceNodeID: api.LocalNodeID, Backend: "codex", Epoch: "epoch"}
			case "admission":
				local.admit = errors.New("native admission denied")
			}
			if _, err := m.StartTask(t.Context(), input("rejected-local-start")); err == nil || local.starts != 0 {
				t.Fatal("unattested source dispatched", err)
			}
			if _, err := os.Stat(m.root); !os.IsNotExist(err) {
				t.Fatal("unattested source allocated a workspace", err)
			}
		})
	}
}
