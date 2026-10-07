package backend

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type remoteRecoveryEngine struct {
	api.Engine
	mu    sync.Mutex
	state api.RecoveryState
}

func (e *remoteRecoveryEngine) RecoveryState() api.RecoveryState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}
func (*remoteRecoveryEngine) Snapshot() api.Snapshot {
	return api.Snapshot{Connection: "ready", CanSend: true}
}

func TestRemoteRecoveryFenceRefusesBeforeDispatchAndDiscardsStagedInput(t *testing.T) {
	e := &remoteRecoveryEngine{state: api.RecoveryState{Fence: "native:9", Automatic: true}}
	s := NewService(e, nil, nil, nil, nil)
	called := 0
	s.SetUserSubmitter(func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		called++
		if called == 1 {
			return api.Receipt{}, api.ErrRecoveryPending // adapter observed a later generation
		}
		if in.NativeIngressFence != "native:9" {
			t.Fatal("native owner fence was lost")
		}
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	})
	in := api.Submission{ID: "telegram:123:1", Text: "queued"}
	if _, err := SubmitRemote(t.Context(), s, in, nil); !errors.Is(err, api.ErrRecoveryPending) || called != 0 || len(s.outbox) != 0 {
		t.Fatal("automatic recovery crossed Control preflight")
	}
	e.mu.Lock()
	e.state.Automatic = false
	e.mu.Unlock()
	in.IngressFence = s.RecoveryState().Fence
	if _, err := SubmitRemote(t.Context(), s, in, nil); !errors.Is(err, api.ErrRecoveryPending) || called != 1 || len(s.outbox) != 0 {
		t.Fatal("native generation refusal left a sending bubble or ambiguous receipt")
	}
	if receipt, err := SubmitRemote(t.Context(), s, in, nil); err != nil || receipt.Outcome != "accepted" || called != 2 {
		t.Fatal("same original request did not submit once after proven predispatch refusal", receipt, err)
	}
	if _, err := SubmitRemote(t.Context(), s, api.Submission{ID: "telegram:123:2", Text: "stale", IngressFence: in.IngressFence + ":stale"}, nil); !errors.Is(err, api.ErrRecoveryPending) || called != 2 {
		t.Fatal("changed Control generation crossed ingress fence")
	}
}

func TestRemoteInputUsesResidentAdmissionAndPreservesDesktopDraft(t *testing.T) {
	consumed := 0
	calls := 0
	s := NewService(snapshotEngine{}, func([]string) ([]api.InputFile, error) { t.Fatal("remote accessed desktop selection"); return nil, nil }, func([]string) error { consumed++; return nil }, nil, nil)
	s.SaveDraft(api.Draft{Text: "unfinished desktop draft"})
	s.SetUserSubmitter(func(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		calls++
		if in.Text != "phone input" || len(files) != 1 {
			t.Fatal("remote submission changed")
		}
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	})
	receipt, e := SubmitRemote(t.Context(), s, api.Submission{ID: "phone-1", Text: "phone input"}, []api.InputFile{{Name: "photo.jpg", Path: "native-owned-path"}})
	if e != nil || receipt.Outcome != "accepted" || calls != 1 || consumed != 0 || s.Draft().Text != "unfinished desktop draft" || s.pendingDraft != nil {
		t.Fatal("second client altered composer or bypassed resident")
	}
	s.restarting = true
	if _, e = SubmitRemote(t.Context(), s, api.Submission{ID: "phone-2"}, nil); e == nil || calls != 1 {
		t.Fatal("remote bypassed runtime restart fence")
	}
}
