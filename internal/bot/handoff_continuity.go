package bot

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Register human ingress before waiting for files, native admission or a
// renewal. Worker observations use SubmitSystem and do not invalidate a handoff.
func (r *Runtime) BeginUserActivity() (func(), error) {
	r.continuity.Lock()
	r.userWaiting.Add(1)
	r.userGeneration++
	generation := r.userGeneration
	r.registeringUser++
	var err error
	if r.handoff != nil {
		err = r.handoff.supersede()
	}
	r.continuity.Unlock()

	r.mu.Lock()
	engine := r.engine
	r.mu.Unlock()
	var state api.ConversationState
	if native, ok := engine.(api.ConversationRuntime); ok {
		state = native.ConversationState()
	}
	r.continuity.Lock()
	if generation == r.userGeneration {
		r.lastHumanTurn = state
	}
	r.registeringUser--
	r.continuity.Unlock()
	return func() { r.userWaiting.Add(-1) }, err
}

func (r *Runtime) checkDreamCompletion(invocation api.ToolInvocation) error {
	r.continuity.Lock()
	defer r.continuity.Unlock()
	pending := r.handoff.pending()
	if pending.CallID != invocation.CallID || pending.Source != invocation.Session || pending.Turn != invocation.Turn || pending.Superseded || r.registeringUser != 0 {
		return api.ErrConversationRenewalDeferred
	}
	return nil
}

func (r *Runtime) guardRenewal(ctx context.Context, pending handoffState) context.Context {
	check := func() error {
		current := r.handoff.pending()
		if current.CallID != pending.CallID || current.Source != pending.Source || current.Turn != pending.Turn || current.Superseded || r.userWaiting.Load() != 0 {
			return api.ErrConversationRenewalDeferred
		}
		return nil
	}
	ctx = api.WithRenewalGuard(ctx, func() error {
		// Required memory is read outside the host commit lock and native locks.
		digest, err := r.dreamMemoryDigest(ctx)
		if err != nil || pending.MemoryDigest != "" && digest != pending.MemoryDigest {
			return errors.Join(api.ErrConversationRenewalDeferred, err)
		}
		return check()
	})
	return api.WithRenewalCommit(ctx, func(session string, adopt func() error) error {
		r.continuity.Lock()
		defer r.continuity.Unlock()
		if err := check(); err != nil {
			return err
		}
		if err := adopt(); err != nil {
			return err
		}
		// Native admission is still held. No new input can consume MEMORY alone
		// between binding publication and this private handoff becoming ready.
		return r.handoff.record(pending.CallID, session, "committed")
	})
}

func (r *Runtime) dreamMemoryDigest(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.dream == nil {
		return "", nil
	}
	seed, err := r.dream.vault.PrepareContext(ctx)
	if err != nil {
		return "", err
	}
	return handoffDigest(seed.Text), nil
}
