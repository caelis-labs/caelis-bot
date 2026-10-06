package bot

import (
	"context"
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type movingRecoveryEngine struct {
	calls int
	input int
}

func (e *movingRecoveryEngine) RecoveryState() api.RecoveryState {
	e.calls++
	return api.RecoveryState{Fence: "owner:9", Automatic: e.calls > 1}
}
func (*movingRecoveryEngine) Snapshot() api.Snapshot { return api.Snapshot{Connection: "ready"} }
func (e *movingRecoveryEngine) Submit(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
	e.input++
	return api.Receipt{}, nil
}

func TestRemoteIngressRecoveryDuringPredispatchRejectionRemainsDeferred(t *testing.T) {
	e := &movingRecoveryEngine{}
	r := &Runtime{engine: e, paused: true}
	_, err := r.SubmitUser(t.Context(), api.Submission{ID: "telegram:123:1", Text: "queued", NativeIngressFence: "owner:9"}, nil)
	if !errors.Is(err, api.ErrRecoveryPending) || e.calls != 2 || e.input != 0 {
		t.Fatal("handoff rejection consumed the original queued input after recovery began", err, e.calls, e.input)
	}
}
