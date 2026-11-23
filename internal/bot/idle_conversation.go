package bot

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const idleConversationAge = 48 * time.Hour

type idleConversation struct {
	activity    func(context.Context, string) (time.Time, bool, error)
	last        time.Time
	lastRequest string
}

// ConfigureIdleConversation enables foreground-only renewal on the product
// resident. Historical Dream records can still supply an already committed seed.
func (r *Runtime) ConfigureIdleConversation(activity func(context.Context, string) (time.Time, bool, error)) {
	r.step.Lock()
	defer r.step.Unlock()
	r.idle = &idleConversation{activity: activity}
}

func (r *Runtime) submitIdleConversation(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	idle := r.idle
	now := r.now()
	conversation, observable := r.engine.(api.ConversationRuntime)
	renew, supported := r.engine.(api.IdleConversationRenewal)
	known := in.ID == idle.lastRequest
	realUser := !in.HostNotice && !in.Scheduled && !in.Dream && in.ModelInputOverride == ""
	if !known && realUser && observable && supported && r.initializationReady() && idle.activity != nil {
		read, cancel := context.WithTimeout(ctx, 2*time.Second)
		latest, alreadyRecorded, err := idle.activity(read, in.ID)
		cancel()
		known = alreadyRecorded
		if idle.last.After(latest) {
			latest = idle.last
		}
		head := conversation.ConversationState()
		if err == nil && !known && !latest.IsZero() && !now.Before(latest.Add(idleConversationAge)) &&
			head.Observed && head.Session != "" && r.engine.Snapshot().CanSend {
			attempt, stop := context.WithTimeout(ctx, 8*time.Second)
			id := fmt.Sprintf("idle-%x", sha256.Sum256([]byte(in.ID)))
			_ = renew.RenewIdleConversation(attempt, id, head)
			stop()
		}
	}
	receipt, err := r.engine.Submit(ctx, in, files)
	if !known && realUser && receipt.ID == in.ID && receipt.Outcome == "accepted" {
		idle.last, idle.lastRequest = now, in.ID
	}
	return receipt, err
}

func (r *Runtime) initializationReady() bool {
	return r.initialization == nil || r.initialization.Initialization().Status == "accepted"
}
