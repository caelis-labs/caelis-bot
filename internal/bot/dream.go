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
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

const dreamIdle = 15 * time.Minute

type dreamAttempt struct {
	ID, Session, Turn, Outcome string
	Started                    time.Time
	Done, Ready                bool
}
type dreamState struct {
	Version  int
	Activity string
	At       time.Time
	Dirty    bool
	Attempt  *dreamAttempt
}
type dreamController struct {
	path, skill string
	vault       *notebook.Vault
	state       dreamState
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
	return nil
}
func (d *dreamController) save() error { return localstate.Write(d.path, d.state) }
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
	if !current.Observed {
		return nil
	}
	now := r.now()
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
		d.state.Activity, d.state.At = conversationActivity(current), now
		d.state.Dirty = current.Turn != "" && current.Turn != a.Turn && receipt.Outcome == "accepted"
		return errors.Join(handoffErr, d.save())
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
	if !dispatch || !current.Idle || !d.state.Dirty || now.Sub(d.state.At) < dreamIdle {
		return nil
	}
	path, err := d.vault.PrepareDream()
	if err != nil {
		return err
	}
	a := &dreamAttempt{ID: "dream-" + rand.Text(), Session: current.Session, Started: now, Outcome: "unknown"}
	d.state.Attempt, d.state.Dirty = a, false
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

// SubmitUser is the sole user-input hook. A completed Dream is a promise to
// rotate on this boundary, not permission to create idle sessions in the timer.
func (r *Runtime) SubmitUser(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	r.step.Lock()
	defer r.step.Unlock()
	rejected := api.Receipt{ID: in.ID, Outcome: "rejected"}
	if r.paused {
		rejected.Message = "应用正在更新，请稍后发送"
		return rejected, nil
	}
	if err := r.tickDream(ctx, false); err != nil {
		rejected.Message = "交接状态暂不可用，请重试"
		return rejected, nil
	}
	if p, ok := r.engine.(api.ConversationRuntime); ok && r.dream != nil {
		d := r.dream
		if a := d.state.Attempt; a != nil {
			if !p.ConversationState().Observed {
				rejected.Message = "正在恢复对话，消息未发送，请稍后重试"
				return rejected, nil
			}
			if !a.Done {
				if err := p.CancelDream(ctx, a.ID); err != nil {
					rejected.Message = "正在结束上下文整理，请稍后重试"
					return rejected, nil
				}
				a.Done, a.Ready = true, false
			}
			if a.Ready {
				if err := p.RenewConversation(ctx, a.ID, a.Session); err != nil && !errors.Is(err, api.ErrConversationRenewalRejected) {
					rejected.Message = "新上下文尚未准备好，消息未发送，请重试"
					return rejected, nil
				}
				a.Ready = false
			}
			if err := d.save(); err != nil {
				rejected.Message = "交接记录保存失败，消息未发送"
				return rejected, nil
			}
		}
	}
	return r.engine.Submit(ctx, in, files)
}
