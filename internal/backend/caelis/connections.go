package caelis

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Connections retains only short-lived user settings interactions. Durable
// effects and receipts stay in Caelis. Secrets are never fields of a flow.
type Connections struct {
	mu      sync.Mutex
	flows   map[string]*connectionFlow
	closed  bool
	workers sync.WaitGroup
}
type connectionFlow struct {
	mu                   sync.Mutex
	view                 api.RuntimeFlow
	changed              chan struct{}
	client               *client
	ctx                  context.Context
	cancel               context.CancelFunc
	created              time.Time
	request              api.RuntimeConnectionInput
	revision             wire.Uint64Decimal
	preparation          wire.ACPPreparation
	launcher             wire.ACPLauncherChoice
	install              wire.RuntimeInstallation
	operation, challenge string
	pending              bool
}

func (f *connectionFlow) expireLocked() {
	if f.ctx.Err() == nil || f.view.Stage == "complete" || f.view.Stage == "failed" || f.view.Stage == "unknown" {
		return
	}
	f.view.Stage = "failed"
	f.view.Title = "连接过程已过期"
	f.view.Message = "请刷新连接后重新开始。"
	if f.pending {
		f.view.Stage = "unknown"
		f.view.Message = "操作结果未确认，请刷新连接核对，不要重复提交。"
	}
	f.view.Authorization = nil
	f.publishLocked()
}
func (f *connectionFlow) snapshot() api.RuntimeFlow {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expireLocked()
	return f.cloneLocked()
}
func (f *connectionFlow) cloneLocked() api.RuntimeFlow {
	b, _ := json.Marshal(f.view)
	var copy api.RuntimeFlow
	_ = json.Unmarshal(b, &copy)
	// An actionable step is visible only after its producing native call has
	// completed. Browser input is the one operation allowed during a pending call.
	if f.pending && (copy.Stage == "launcher" || copy.Stage == "models" || copy.Stage == "installation" || copy.Stage == "auth-method") {
		copy.Stage = "preparing"
		copy.Title = "正在准备连接"
	}
	return copy
}
func (f *connectionFlow) publishLocked() {
	f.view.Sequence++
	f.view.Revision = strconv.Itoa(f.view.Sequence)
	close(f.changed)
	f.changed = make(chan struct{})
}
func (f *connectionFlow) update(change func(*api.RuntimeFlow)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(&f.view)
	f.publishLocked()
}
func (f *connectionFlow) unknown() {
	f.update(func(v *api.RuntimeFlow) {
		if v.Stage == "complete" {
			return
		}
		v.Stage = "unknown"
		v.Title = "操作结果尚未确认"
		v.Message = "请刷新连接核对结果，不要重复提交。"
		v.Authorization = nil
	})
}
func (f *connectionFlow) result(result wire.CommandResult, err error) bool {
	if err != nil || result.OperationId != f.operation {
		f.unknown()
		return false
	}
	if !succeeded(result.Outcome) {
		r := mutationResult(f.operation, result, nil)
		f.update(func(v *api.RuntimeFlow) {
			v.Stage = "failed"
			if r.Outcome == "unknown" || r.Outcome == "accepted" {
				v.Stage = "unknown"
			}
			v.Title = "连接尚未完成"
			v.Message = r.Message
			v.Authorization = nil
		})
		return false
	}
	return true
}
func (f *connectionFlow) complete() {
	f.update(func(v *api.RuntimeFlow) {
		v.Stage = "complete"
		v.Title = "连接已添加"
		v.Message = "现在可以为模型选择用途。"
		v.Authorization = nil
	})
}
func (s *Connections) lookup(id string) (*connectionFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.flows[id]
	if f == nil {
		return nil, errors.New("此连接过程已结束或过期，请刷新连接列表")
	}
	return f, nil
}
func ConnectionCatalog(ctx context.Context, settings api.RuntimeSettings, kind string) (api.RuntimeConnectionCatalog, error) {
	out := api.RuntimeConnectionCatalog{Choices: []api.RuntimeConnectChoice{}}
	command := map[string]string{"account": "connect-provider-account", "api-key": "connect-provider-api-key", "agent": "connect-acp-agent"}[kind]
	if command == "" {
		return out, errors.New("不支持的连接类型")
	}
	c, err := setupClient(ctx, settings)
	if err != nil {
		return out, err
	}
	defer c.http.CloseIdleConnections()
	if kind == "account" {
		info, e := initialize(ctx, c)
		if e != nil {
			return out, e
		}
		available := false
		for _, cap := range info.Capabilities {
			if cap == "model-auth-stream-v1" {
				available = true
			}
		}
		if !available {
			out.Unavailable = "请更新并重启本机 Caelis，以使用浏览器授权连接。"
			return out, nil
		}
	}
	candidates, err := rawCatalog(ctx, c, command)
	if err != nil {
		return out, err
	}
	for _, choice := range candidates {
		out.Choices = append(out.Choices, api.RuntimeConnectChoice{ID: choice.Value, Name: value(choice.Display), Description: value(choice.Detail), Custom: kind == "agent" && choice.Value == "custom"})
	}
	return out, nil
}
func (s *Connections) Start(ctx context.Context, settings api.RuntimeSettings, input api.RuntimeConnectionInput) (api.RuntimeFlow, error) {
	catalog, err := ConnectionCatalog(ctx, settings, input.Kind)
	if err != nil {
		return api.RuntimeFlow{}, err
	}
	valid := false
	for _, c := range catalog.Choices {
		if c.ID == input.Choice {
			valid = true
		}
	}
	if !valid {
		return api.RuntimeFlow{}, errors.New("连接项目已不可用，请刷新目录")
	}
	c, err := setupClient(ctx, settings)
	if err != nil {
		return api.RuntimeFlow{}, err
	}
	c.http.Transport.(*http.Transport).ResponseHeaderTimeout = 0
	status, err := hostRevision(ctx, c)
	if err != nil {
		c.http.CloseIdleConnections()
		return api.RuntimeFlow{}, err
	}
	if len(input.APIKey) > 16384 {
		c.http.CloseIdleConnections()
		return api.RuntimeFlow{}, errors.New("API Key 长度无效")
	}
	secret := input.APIKey
	input.APIKey = ""
	flowCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	f := &connectionFlow{client: c, ctx: flowCtx, cancel: cancel, changed: make(chan struct{}), created: time.Now(), request: input, revision: status.Configuration.Revision, view: api.RuntimeFlow{ID: rand.Text(), Stage: "preparing", Title: "正在准备连接", Message: "由本机 Caelis 检查连接与认证。", Methods: []api.RuntimeAuthMethod{}, Models: []api.RuntimeFlowModel{}}}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		c.http.CloseIdleConnections()
		return api.RuntimeFlow{}, errors.New("设置已关闭")
	}
	if s.flows == nil {
		s.flows = map[string]*connectionFlow{}
	}
	for id, old := range s.flows {
		if time.Since(old.created) > 15*time.Minute {
			old.cancel()
			old.client.http.CloseIdleConnections()
			delete(s.flows, id)
		}
	}
	if len(s.flows) >= 32 {
		s.mu.Unlock()
		cancel()
		c.http.CloseIdleConnections()
		return api.RuntimeFlow{}, errors.New("连接操作过多，请稍后重试")
	}
	s.flows[f.view.ID] = f
	s.mu.Unlock()
	s.run(f, func() {
		if input.Kind == "agent" {
			f.startAgent()
		} else if input.Kind == "account" {
			f.accountModels()
		} else {
			f.connectProvider(input.Model, secret, false)
		}
	})
	return f.snapshot(), nil
}
func (s *Connections) run(f *connectionFlow, work func()) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		f.unknown()
		return
	}
	s.workers.Add(1)
	s.mu.Unlock()
	f.mu.Lock()
	f.pending = true
	f.view.Stage = "preparing"
	f.view.Authorization = nil
	f.publishLocked()
	f.mu.Unlock()
	go func() {
		defer s.workers.Done()
		defer func() { f.mu.Lock(); f.pending = false; f.publishLocked(); f.mu.Unlock() }()
		work()
	}()
}
func (s *Connections) Wait(ctx context.Context, id string, after int) (api.RuntimeFlow, error) {
	f, err := s.lookup(id)
	if err != nil {
		return api.RuntimeFlow{}, err
	}
	f.mu.Lock()
	changed := f.changed
	view := f.cloneLocked()
	f.mu.Unlock()
	if view.Sequence > after {
		return view, nil
	}
	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return api.RuntimeFlow{}, ctx.Err()
	case <-changed:
	case <-f.ctx.Done():
	case <-timer.C:
	}
	return f.snapshot(), nil
}
func (s *Connections) Cancel(_ context.Context, id string) error {
	f, err := s.lookup(id)
	if err != nil {
		return err
	}
	f.mu.Lock()
	pending := f.pending
	f.cancel()
	if pending && f.view.Stage != "complete" {
		f.view.Stage = "unknown"
		f.view.Title = "已请求取消"
		f.view.Message = "正在核对已发生的操作，请刷新连接列表。"
	} else if f.view.Stage != "complete" && f.view.Stage != "unknown" {
		f.view.Stage = "failed"
		f.view.Title = "连接已取消"
	}
	f.view.Authorization = nil
	f.publishLocked()
	f.mu.Unlock()
	return nil
}
func (s *Connections) Close() {
	s.mu.Lock()
	s.closed = true
	for _, f := range s.flows {
		f.cancel()
		f.client.http.CloseIdleConnections()
	}
	s.mu.Unlock()
	s.workers.Wait()
}
func (s *Connections) Advance(ctx context.Context, action api.RuntimeFlowAction) (api.RuntimeFlow, error) {
	f, err := s.lookup(action.ID)
	if err != nil {
		return api.RuntimeFlow{}, err
	}
	if action.Action == "refresh" {
		return f.snapshot(), nil
	}
	f.mu.Lock()
	f.expireLocked()
	view := f.cloneLocked()
	pending := f.pending
	if view.Revision != action.Revision {
		f.mu.Unlock()
		return view, nil
	}
	if action.Action == "submit-code" && view.Stage == "authorization" && f.challenge != "" && view.Authorization != nil && view.Authorization.CanSubmit {
		if len(action.Input.Code) > 16384 || strings.TrimSpace(action.Input.Code) == "" {
			f.mu.Unlock()
			return view, errors.New("授权信息长度无效")
		}
		challenge, operation := f.challenge, f.operation
		f.challenge = ""
		f.view.Authorization.CanSubmit = false
		f.publishLocked()
		f.mu.Unlock()
		err := f.client.json(ctx, "POST", "/configuration/operations/"+idPath(operation)+"/auth-input", wire.ModelAuthenticationInput{ChallengeId: challenge, Input: action.Input.Code}, nil, "", "")
		if err != nil {
			f.unknown()
		}
		return f.snapshot(), nil
	}
	if pending || view.Stage == "unknown" || view.Stage == "complete" || view.Stage == "failed" {
		f.mu.Unlock()
		return view, errors.New("当前连接状态不允许此操作，请核对结果")
	}
	f.mu.Unlock()
	var work func()
	switch {
	case view.Stage == "launcher" && action.Action == "choose-launcher":
		valid := false
		for _, launcher := range view.Launchers {
			if launcher.ID == action.Input.Launcher {
				valid = true
			}
		}
		if !valid {
			return view, errors.New("请选择当前 Agent 提供的启动方式")
		}
		work = func() {
			f.launcher = wire.ACPLauncherChoice(action.Input.Launcher)
			f.prepareAgent(wire.ACPPrepareRequest{AdapterId: &f.request.Choice, Launcher: f.launcher})
		}
	case view.Stage == "installation" && (action.Action == "install" || action.Action == "check-installation"):
		work = func() { f.prepareInstalled(action.Action, action.Input.Destination) }
	case view.Stage == "auth-method" && action.Action == "authenticate":
		valid := false
		for _, method := range view.Methods {
			if method.ID == action.Input.Method && method.Available {
				valid = true
			}
		}
		if !valid {
			return view, errors.New("此认证方式不可用")
		}
		work = func() { f.authenticateAgent(action.Input.Method) }
	case view.Stage == "models" && action.Action == "connect":
		valid := false
		for _, model := range view.Models {
			if model.ID == action.Input.Model {
				valid = true
			}
		}
		if !valid {
			return view, errors.New("请选择当前连接提供的模型")
		}
		if f.request.Kind == "agent" {
			work = func() { f.connectAgent(action.Input.Model) }
		} else {
			work = func() { f.connectProvider(action.Input.Model, "", true) }
		}
	default:
		return view, errors.New("连接步骤已变化，请重新核对")
	}
	// Fence double clicks before starting any native command.
	f.mu.Lock()
	if f.view.Revision != action.Revision || f.pending {
		current := f.cloneLocked()
		f.mu.Unlock()
		return current, nil
	}
	f.pending = true
	f.publishLocked()
	f.mu.Unlock()
	s.run(f, work)
	return f.snapshot(), nil
}
