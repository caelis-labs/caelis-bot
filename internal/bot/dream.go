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
			return errors.New("old Dream receipt is unreadable; original file retained")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	r.dream = d
	return r.openHandoff()
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
			if !a.Done {
				if err := p.CancelDream(ctx, a.ID); err != nil {
					rejected.Message = "正在核对旧整理请求，请稍后重试"
					return reject()
				}
				a.Done, a.Ready = true, false
			}
			if a.Ready {
				text, err := d.vault.LegacyDreamHandoff(a.ID)
				if err != nil || r.handoff == nil {
					rejected.Message = "旧交接尚未准备好，消息未发送，请重试"
					return reject()
				}
				if err := r.handoff.save(a.ID, a.Session, text); err != nil {
					rejected.Message = "旧交接记录保存失败，消息未发送，请重试"
					return reject()
				}
				err = p.RenewConversation(ctx, a.ID, a.Session)
				if err != nil && !errors.Is(err, api.ErrConversationRenewalRejected) {
					rejected.Message = "新上下文尚未准备好，消息未发送，请重试"
					return reject()
				}
				if errors.Is(err, api.ErrConversationRenewalRejected) {
					if clearErr := r.handoff.clear(a.ID); clearErr != nil {
						rejected.Message = "旧交接记录清理失败，消息未发送，请重试"
						return reject()
					}
				}
				a.Ready = false
			}
			if err := d.save(); err != nil {
				rejected.Message = "交接记录保存失败，消息未发送"
				return reject()
			}
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
