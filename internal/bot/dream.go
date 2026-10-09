package bot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

// These types read the already dispatched host maintenance receipt from earlier
// versions. No new maintenance turn is created, including after an upgrade.
type dreamAttempt struct {
	ID, Session, Turn, Outcome string
	UpgradeVersion             string
	Started                    time.Time
	Done, Ready                bool
}
type dreamState struct {
	Version         int
	Activity        string
	At              time.Time
	Dirty           bool
	Attempt         *dreamAttempt
	LastAttempt     time.Time
	BaselineSession string
	BaselineUsed    int64
}
type dreamController struct {
	path  string
	vault *notebook.Vault
	state dreamState
}

func (r *Runtime) ConfigureDream(vault *notebook.Vault, _ string) error {
	if !r.step.TryLock() {
		return errors.New("Dream receipt recovery waits for current work")
	}
	defer r.step.Unlock()
	if vault == nil {
		return errors.New("Dream receipt recovery needs the Notebook")
	}
	d := &dreamController{path: filepath.Join(filepath.Dir(r.path), "dream-"+r.provider+".json"), vault: vault, state: dreamState{Version: 1}}
	b, err := os.ReadFile(d.path)
	if err == nil {
		if json.Unmarshal(b, &d.state) != nil || d.state.Version != 1 {
			// A corrupt retired scheduler file cannot disable ordinary Bot work.
			// Leave the original bytes untouched for an explicit later repair.
			d.state = dreamState{Version: 1}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		d.state = dreamState{Version: 1}
	}
	r.dream = d
	if err := r.openHandoff(); err != nil {
		// Dream stays unavailable while its original private record remains
		// unreadable; the bound conversation and other tools stay available.
		r.handoff = nil
	}
	return nil
}
func (d *dreamController) save() error { return localstate.Write(d.path, d.state) }

// reconcileLegacyDream observes only an original, already dispatched receipt.
// It never submits maintenance, makes an upgrade-triggered new session, or
// resolves unknown work by replaying it.
func (r *Runtime) reconcileLegacyDream() error {
	d := r.dream
	p, ok := r.engine.(api.ConversationRuntime)
	if d == nil || !ok || d.state.Attempt == nil || d.state.Attempt.Done {
		return nil
	}
	a := d.state.Attempt
	receipt, result := p.DreamResult(a.ID)
	if receipt.Outcome == "" || receipt.Outcome == "unknown" {
		return nil
	}
	if receipt.Outcome == "accepted" && (result.Status == "" || result.Status == "running" || result.Status == "inProgress" || result.Status == "started") {
		return nil
	}
	a.Outcome, a.Turn, a.Done = receipt.Outcome, result.Turn, true
	if receipt.Outcome == "accepted" && result.Status == "completed" {
		ready, err := d.vault.DreamReady(a.ID)
		if err != nil {
			return err
		}
		current := p.ConversationState()
		a.Ready = ready && current.Session == a.Session && current.Turn == result.Turn
	}
	return d.save()
}

// SubmitUser shares admission with schedule and report delivery. A legacy
// completed handoff can rotate on this input; rejection falls back to the old
// session, while an unknown creation stays on its original receipt.
func (r *Runtime) SubmitUser(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	r.step.Lock()
	defer r.step.Unlock()
	if r.remoteRecoveryPending(in) {
		return api.Receipt{}, api.ErrRecoveryPending
	}
	rejected := api.Receipt{ID: in.ID, Outcome: "rejected"}
	reject := func() (api.Receipt, error) {
		if r.remoteRecoveryPending(in) {
			return api.Receipt{}, api.ErrRecoveryPending
		}
		return rejected, nil
	}
	if r.paused {
		rejected.Message = "应用正在更新，请稍后发送"
		return reject()
	}
	if err := r.reconcileLegacyDream(); err != nil {
		// A bad historical file cannot disable the current bound conversation.
		// Retain the original receipt on disk for a later repair/restart.
		if r.dream != nil && r.dream.state.Attempt != nil {
			r.dream.state.Attempt.Done = true
			r.dream.state.Attempt.Ready = false
		}
	}
	if r.handoff != nil {
		pending := r.handoff.pending()
		if pending.CallID != "" && pending.Turn != "" && pending.NewSession == "" {
			if err := r.renewToolHandoff(ctx); err != nil {
				// Preserve the original invocation and summary in its private
				// receipt. A failed create must not make the bound Bot unusable.
				if fallbackErr := r.handoff.record(pending.CallID, "", "fallback"); fallbackErr != nil && r.handoff.pending().CallID != "" {
					rejected.Message = "交接状态尚未保存，消息未发送，请重试"
					return reject()
				}
			}
		}
	}
	if p, ok := r.engine.(api.ConversationRuntime); ok && r.dream != nil {
		d := r.dream
		if a := d.state.Attempt; a != nil {
			current := p.ConversationState()
			if !current.Observed {
				rejected.Message = "正在恢复对话，消息未发送，请稍后重试"
				return reject()
			}
			if a.Ready && (current.Session != a.Session || current.Turn != a.Turn || current.Status != "completed") {
				if r.handoff != nil && current.Session == a.Session {
					_ = r.handoff.clear(a.ID)
				}
				a.Ready = false
			}
			if !a.Done && !current.Idle {
				if err := p.CancelDream(ctx, a.ID); err != nil {
					rejected.Message = "正在核对旧整理请求，请稍后重试"
					return reject()
				}
				a.Done, a.Ready = true, false
			}
			if a.Ready {
				text, err := d.vault.LegacyDreamHandoff(a.ID)
				if err != nil || r.handoff == nil {
					a.Ready = false // keep the old usable Session and the Notebook file
				} else if err := r.handoff.save(a.ID, a.Session, text); err != nil {
					a.Ready = false // private persistence failed before any native create
				} else {
					err = p.RenewConversation(ctx, a.ID, a.Session)
					if err != nil && !errors.Is(err, api.ErrConversationRenewalRejected) {
						rejected.Message = "新上下文尚未准备好，消息未发送，请重试"
						return reject()
					}
					if errors.Is(err, api.ErrConversationRenewalRejected) {
						_ = r.handoff.record(a.ID, "", "rejected")
					} else if err == nil {
						if recordErr := r.handoff.record(a.ID, p.ConversationState().Session, "committed"); recordErr != nil {
							rejected.Message = "交接结果尚未保存，消息未发送，请重试"
							return reject()
						}
					}
					a.Ready = false
				}
			}
			_ = d.save() // native binding and original receipt remain authoritative
		}
	}
	return r.engine.Submit(ctx, in, files)
}

func (r *Runtime) remoteRecoveryPending(in api.Submission) bool {
	if in.NativeIngressFence == "" {
		return false
	}
	source, ok := r.engine.(api.RecoverySource)
	if !ok {
		return false
	}
	state := source.RecoveryState()
	return state.Automatic || state.InProgress || state.Fence != in.NativeIngressFence
}
