package bot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

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
	path, skill string
	vault       *notebook.Vault
	state       dreamState
	policy      dreamPolicy
	log         *diagnosticlog.Logger
}

func (r *Runtime) ConfigureDream(vault *notebook.Vault, coreSkill string) error {
	r.step.Lock()
	defer r.step.Unlock()
	if vault == nil {
		return errors.New("Dream 需要可写笔记本")
	}
	d := &dreamController{path: filepath.Join(filepath.Dir(r.path), "dream-"+r.provider+".json"), vault: vault,
		skill: filepath.Join(filepath.Dir(filepath.Dir(coreSkill)), "caelis-dream", "SKILL.md"), state: dreamState{Version: 1}}
	b, err := os.ReadFile(d.path)
	if err == nil {
		if json.Unmarshal(b, &d.state) != nil || d.state.Version != 1 {
			return errors.New("Dream 记录无法读取，已保留原文件")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	r.dream = d
	if d.state.LastAttempt.IsZero() && d.state.Attempt != nil {
		d.state.LastAttempt = d.state.Attempt.Started
	}
	d.policy.notBefore = r.now().Round(0)
	return nil
}
func (d *dreamController) save() error { return localstate.Write(d.path, d.state) }

func (r *Runtime) ConfigureDreamDiagnostics(log *diagnosticlog.Logger) {
	r.step.Lock()
	defer r.step.Unlock()
	if r.dream != nil {
		r.dream.log = log
	}
}
func conversationActivity(s api.ConversationState) string {
	return s.Session + "\x00" + s.Turn + "\x00" + s.Status
}

// tickDream runs under the same admission lock as user input and resident
// wakeups. The provider only supplies exact ordinary turn/session receipts.
func (r *Runtime) tickDream(ctx context.Context, dispatch bool) error {
	d := r.dream
	p, ok := r.engine.(api.ConversationRuntime)
	if d == nil || !ok {
		return nil
	}
	current := p.ConversationState()
	now := r.now().Round(0)
	stable := d.policy.observe(now, current)
	if !current.Observed {
		return nil
	}
	upgrade := current.DesiredRuntimeVersion != "" && current.RuntimeVersion != current.DesiredRuntimeVersion
	if a := d.state.Attempt; a != nil && !a.Done {
		receipt, result := p.DreamResult(a.ID)
		if receipt.Outcome == "unknown" || receipt.Outcome == "" {
			return nil
		}
		a.Outcome = receipt.Outcome
		if receipt.Outcome == "accepted" && (result.Status == "inProgress" || result.Status == "running" || result.Status == "started" || result.Status == "") {
			if now.Sub(a.Started) > 2*time.Minute {
				cancelCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := p.CancelDream(cancelCtx, a.ID)
				cancel()
				if err != nil {
					return err
				}
			}
			return nil
		}
		a.Turn = result.Turn
		var handoffErr error
		if receipt.Outcome == "accepted" && result.Status == "completed" {
			ready, err := d.vault.DreamReady(a.ID)
			handoffErr = err
			a.Ready = err == nil && ready && current.Session == a.Session && current.Turn == a.Turn
		}
		a.Done = true
		if current.Session == a.Session && current.Turn == a.Turn && current.Usage.Window > 0 {
			d.state.BaselineSession, d.state.BaselineUsed = current.Session, current.Usage.Used
			d.policy.baseline = current.Usage.Used
		}
		d.state.Activity, d.state.At = conversationActivity(current), now
		d.state.Dirty = current.Turn != "" && current.Turn != a.Turn && receipt.Outcome == "accepted"
		if err := errors.Join(handoffErr, d.save()); err != nil {
			return err
		}
		if upgrade && a.Ready {
			return d.renew(ctx, p)
		}
		return nil
	}
	if a := d.state.Attempt; upgrade && a != nil && a.Ready && current.Session == a.Session && current.Turn == a.Turn && current.Status == "completed" {
		return d.renew(ctx, p)
	}
	key := conversationActivity(current)
	if key != d.state.Activity {
		d.state.Activity, d.state.At = key, now
		d.state.Dirty = current.Turn != ""
		if d.state.Attempt != nil {
			d.state.Attempt.Ready = false
		}
		if err := d.save(); err != nil {
			return err
		}
	}
	// An upgrade is a finite startup handoff, not another periodic model loop.
	// A rejected/interrupted attempt waits for normal activity and idle maintenance.
	startup := upgrade && (d.state.Attempt == nil || d.state.Attempt.UpgradeVersion != current.DesiredRuntimeVersion)
	reason := d.policy.decide(now, current, d.state)
	if dispatch && reason != d.policy.reason && d.state.Dirty {
		age := int64(-1)
		if !current.Usage.ModelAt.IsZero() {
			age = int64(now.Sub(current.Usage.ModelAt).Seconds())
		}
		d.log.Write(diagnosticlog.Record{Level: "info", Component: "dream", Code: "admission_" + reason,
			Reason: fmt.Sprintf("context_used=%d context_window=%d model_age_seconds=%d", current.Usage.Used, current.Usage.Window, age)})
	}
	d.policy.reason = reason
	if !dispatch || !current.Idle || (startup && d.policy.sample != nil && !stable) || (!startup && d.policy.reason != "ready") {
		return nil
	}
	path, err := d.vault.PrepareDream()
	if err != nil {
		return err
	}
	a := &dreamAttempt{ID: "dream-" + rand.Text(), Session: current.Session, Started: now, Outcome: "unknown"}
	if upgrade {
		a.UpgradeVersion = current.DesiredRuntimeVersion
	}
	d.state.Attempt, d.state.Dirty = a, false
	d.state.LastAttempt = now
	d.state.BaselineSession, d.state.BaselineUsed = current.Session, current.Usage.Used
	d.policy.baseline = current.Usage.Used
	if err := d.save(); err != nil {
		return err
	}
	prompt := fmt.Sprintf("System Dream request from the Bot host. Explicitly load caelis-dream at %q and follow it now. Write the handoff to exactly %q (the host has prepared this writable location), starting with this exact first line:\n%s\nOptionally update useful memory. Finish with only one short user-facing recap sentence. Do not start a new session or continue ordinary work.", d.skill, path, notebook.DreamMarker(a.ID))
	receipt, err := p.SubmitDream(ctx, api.Submission{ID: a.ID, Text: prompt, Dream: true})
	a.Outcome = receipt.Outcome
	if receipt.Outcome == "rejected" {
		a.Done = true
	}
	return errors.Join(err, d.save())
}

// renew shares exact receipt recovery between upgrade and ordinary Dream handoffs.
func (d *dreamController) renew(ctx context.Context, p api.ConversationRuntime) error {
	a := d.state.Attempt
	current := p.ConversationState()
	if current.DesiredRuntimeVersion != "" && current.RuntimeVersion != current.DesiredRuntimeVersion && a.UpgradeVersion != current.DesiredRuntimeVersion {
		a.UpgradeVersion = current.DesiredRuntimeVersion
		if err := d.save(); err != nil {
			return err
		}
	}
	if err := p.RenewConversation(ctx, a.ID, a.Session); err != nil && !errors.Is(err, api.ErrConversationRenewalRejected) {
		return err
	}
	a.Ready = false
	return d.save()
}

// SubmitUser is the sole user-input hook. Ordinary Dream rotates on user input;
// a version upgrade can also rotate at the restored, idle startup boundary.
func (r *Runtime) SubmitUser(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	r.step.Lock()
	defer r.step.Unlock()
	if r.remoteRecoveryPending(in) {
		return api.Receipt{}, api.ErrRecoveryPending
	}
	rejected := api.Receipt{ID: in.ID, Outcome: "rejected"}
	reject := func() (api.Receipt, error) {
		// Dream and handoff checks may race a reconnect after the initial
		// observation. They have not dispatched this user input yet.
		if r.remoteRecoveryPending(in) {
			return api.Receipt{}, api.ErrRecoveryPending
		}
		return rejected, nil
	}
	if r.paused {
		rejected.Message = "应用正在更新，请稍后发送"
		return reject()
	}
	if err := r.tickDream(ctx, false); err != nil {
		rejected.Message = "交接状态暂不可用，请重试"
		return reject()
	}
	if p, ok := r.engine.(api.ConversationRuntime); ok && r.dream != nil {
		d := r.dream
		if a := d.state.Attempt; a != nil {
			if !p.ConversationState().Observed {
				rejected.Message = "正在恢复对话，消息未发送，请稍后重试"
				return reject()
			}
			if !a.Done {
				if err := p.CancelDream(ctx, a.ID); err != nil {
					rejected.Message = "正在结束上下文整理，请稍后重试"
					return reject()
				}
				a.Done, a.Ready = true, false
			}
			if a.Ready {
				if err := d.renew(ctx, p); err != nil {
					rejected.Message = "新上下文尚未准备好，消息未发送，请重试"
					return reject()
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
