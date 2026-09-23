package bot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type introductionState struct {
	Version int    `json:"version"`
	ID      string `json:"id,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	Runtime string `json:"runtime,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Initializer persists one ordinary user message until native acceptance. An
// uncertain send is reconciled, never automatically replayed on another Runtime.
type Initializer struct {
	mu    sync.Mutex
	step  sync.Mutex
	path  string
	state introductionState
}

func OpenInitializer(path string) (*Initializer, error) {
	i := &Initializer{path: path, state: introductionState{Version: 1, Status: "required"}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return i, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &i.state) != nil || i.state.Version != 1 {
		return nil, errors.New("Bot 初始化记录无法读取，请保留数据")
	}
	switch i.state.Status {
	case "required":
	case "pending", "rejected", "dispatching", "unknown", "accepted":
		if i.state.ID == "" || (i.state.Status != "accepted" && i.state.Prompt == "") {
			return nil, errors.New("Bot 初始化回执缺失")
		}
	default:
		return nil, errors.New("Bot 初始化状态无效")
	}
	if (i.state.Status == "dispatching" || i.state.Status == "unknown") && i.state.Runtime == "" {
		return nil, errors.New("Bot 初始化运行时缺失")
	}
	if i.state.Status == "dispatching" {
		i.state.Status = "unknown"
	}
	return i, nil
}
func (i *Initializer) Initialization() api.BotInitialization {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.view()
}
func (i *Initializer) view() api.BotInitialization {
	v := api.BotInitialization{Required: i.state.Status == "required", Status: i.state.Status}
	switch i.state.Status {
	case "rejected":
		v.Message = i.state.Message
		if v.Message == "" {
			v.Message = "介绍未发送，请检查连接后重试。"
		}
	case "pending":
		v.Message = "介绍已保存，连接就绪后会发给 Bot。"
	case "dispatching":
		v.Message = "正在发送介绍…"
	case "unknown":
		v.Message = "介绍的发送结果待确认，请重新连接核对，不会重复发送。"
	}
	return v
}
func (i *Initializer) Initialize(ctx context.Context, in api.BotIntroduction) (api.BotInitialization, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return i.view(), err
	}
	if i.state.Status != "required" {
		return i.view(), nil
	}
	name, description := strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.ContainsAny(name, "\r\n") || !utf8.ValidString(name) || utf8.RuneCountInString(description) > 2000 || !utf8.ValidString(description) {
		return i.view(), errors.New("请填写名字（不超过 80 字），描述可选且不超过 2,000 字")
	}
	prompt := "你的名字是" + name + "。"
	if description != "" {
		prompt += "\n描述是" + description
	}
	next := introductionState{Version: 1, ID: "intro-" + rand.Text(), Prompt: prompt, Status: "pending"}
	if err := localstate.Write(i.path, next); err != nil {
		return i.view(), err
	}
	i.state = next
	return i.view(), nil
}
func (i *Initializer) GuardRuntimeChange() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state.Status == "unknown" || i.state.Status == "dispatching" {
		return errors.New("请先核对 Bot 介绍的发送结果，再切换运行时")
	}
	return nil
}
func (i *Initializer) Deliver(ctx context.Context, engine Engine, provider string) error {
	i.step.Lock()
	defer i.step.Unlock()
	i.mu.Lock()
	switch i.state.Status {
	case "required", "accepted", "rejected":
		i.mu.Unlock()
		return nil
	case "unknown", "dispatching":
		receipt := engine.Snapshot().LastReceipt
		if i.state.Runtime == provider && receipt.ID == i.state.ID && (receipt.Outcome == "accepted" || receipt.Outcome == "rejected") {
			next := i.state
			next.Status = receipt.Outcome
			if receipt.Outcome == "accepted" {
				next.Prompt = ""
			} else {
				next.Message = receipt.Message
				next.Runtime = ""
			}
			if err := localstate.Write(i.path, next); err != nil {
				i.mu.Unlock()
				return err
			}
			i.state = next
		}
		i.mu.Unlock()
		return nil
	}
	if !engine.Snapshot().CanSend {
		i.mu.Unlock()
		return nil
	}
	next := i.state
	next.Status = "dispatching"
	next.Runtime = provider
	if err := localstate.Write(i.path, next); err != nil {
		i.mu.Unlock()
		return err
	}
	i.state = next
	i.mu.Unlock()
	receipt, err := engine.Submit(ctx, api.Submission{ID: next.ID, Text: next.Prompt}, nil)
	i.mu.Lock()
	defer i.mu.Unlock()
	next.Status = "unknown"
	if receipt.ID == next.ID && receipt.Outcome == "accepted" {
		next.Status = "accepted"
		next.Prompt = ""
	} else if err == nil && receipt.ID == next.ID && receipt.Outcome == "rejected" {
		next.Status = "rejected"
		next.Message = receipt.Message
		next.Runtime = ""
	}
	if save := localstate.Write(i.path, next); save != nil {
		return save
	}
	i.state = next
	return err
}

// ConfigureInitialization is called by the product host before Start.
func (r *Runtime) ConfigureInitialization(i *Initializer) { r.initialization = i }

// Retry only a definite rejection, with a fresh transport id. An unknown result
// is never retried through this API and retains its original reconciliation id.
func (i *Initializer) RetryInitialization(ctx context.Context) (api.BotInitialization, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return i.view(), err
	}
	if i.state.Status != "rejected" {
		return i.view(), errors.New("仅明确未发送的介绍可以重试")
	}
	next := i.state
	next.ID = "intro-" + rand.Text()
	next.Status = "pending"
	next.Message = ""
	if err := localstate.Write(i.path, next); err != nil {
		return i.view(), err
	}
	i.state = next
	return i.view(), nil
}
