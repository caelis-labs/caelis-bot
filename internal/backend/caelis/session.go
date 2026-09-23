package caelis

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

type Options struct {
	Directory string
	Settings  api.RuntimeSettings
	// ApplicationOwned disables the retired Control-owned Bot Mode. The new
	// generic wire contract must be integrated before this path can connect.
	ApplicationOwned bool
}
type Session struct {
	applicationOwned  bool
	mu                sync.Mutex
	step              sync.Mutex
	path              string
	settings          api.RuntimeSettings
	state             binding
	loadErr           error
	client            *client
	info              wire.ServerInfo
	connected, closed bool
	issue             string
	revision          uint64
	changed           chan struct{}
	cancel            context.CancelFunc
	ctx               context.Context
	wg                sync.WaitGroup
	streamCtx         context.Context
	streamCancel      context.CancelFunc
	generation        uint64
	wake              chan struct{}
	streams           map[string]bool
	effects           api.DesktopEffects
	works             []wire.BotWork
	completions       []wire.BotCompletion
}

func New(opts Options) *Session {
	p := filepath.Join(opts.Directory, "binding.json")
	b, e := loadBinding(p)
	return &Session{applicationOwned: opts.ApplicationOwned, path: p, settings: opts.Settings, state: b, loadErr: e, revision: 1, changed: make(chan struct{}), streams: map[string]bool{}, wake: make(chan struct{}, 1)}
}
func (*Session) ProviderInfo() api.ProviderInfo {
	return api.ProviderInfo{ID: "caelis", Name: "Caelis", ConnectionKind: "local-host", HelpURL: "https://caelis.dev", ConnectionHint: "使用本机 Caelis Control 服务。可自动查找或选择二进制；安装与更新由 Caelis 官方工具完成。"}
}
func (s *Session) BindDesktop(e api.DesktopEffects) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connected {
		return errors.New("连接后不能替换桌面能力")
	}
	s.effects = e
	return nil
}
func (s *Session) Connect(ctx context.Context) error {
	if s.applicationOwned {
		return s.fail(errApplicationProtocol)
	}
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("连接已停止")
	}
	if s.connected {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if s.loadErr != nil {
		return s.fail(s.loadErr)
	}
	if e := s.connect(ctx); e != nil {
		return s.fail(e)
	}
	s.mu.Lock()
	if s.cancel == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
		s.wg.Add(1)
		go s.pollLoop(s.ctx)
	}
	s.connected = true
	s.issue = ""
	s.bumpLocked()
	s.mu.Unlock()
	return nil
}
func (s *Session) fail(e error) error {
	s.mu.Lock()
	s.connected = false
	s.issue = e.Error()
	s.bumpLocked()
	s.mu.Unlock()
	return e
}

type discovery struct {
	Schema      string `json:"schema_version"`
	Endpoint    string `json:"endpoint"`
	InstanceID  string `json:"instance_id"`
	PrincipalID string `json:"principal_id"`
}

func Discover(settings api.RuntimeSettings) (discovery, string, error) {
	dir, e := caelisruntime.Store(settings.CaelisStore)
	if e != nil {
		return discovery{}, "", e
	}
	raw, e := privateRead(filepath.Join(dir, "runtime/service/discovery.json"), 65536)
	if e != nil {
		return discovery{}, "", errors.New("Caelis 服务尚未就绪，请在连接设置中启动服务")
	}
	var d discovery
	if json.Unmarshal(raw, &d) != nil || d.Schema != "caelis.control.service-discovery/v1" || d.PrincipalID == "" {
		return d, "", errors.New("Caelis 服务发现记录不兼容")
	}
	b, e := privateRead(filepath.Join(dir, "runtime/service/auth.token"), 65536)
	if e != nil {
		return d, "", errors.New("无法读取本机 Caelis 的受保护凭据")
	}
	return d, strings.TrimSpace(string(b)), nil
}

var required = []string{"bot-mode-v1", "bot-private-files-v1", "bot-managed-work-v1", "bot-desktop-actions-v1", "bot-reminder-grants-v1", "bot-text-results-v1"}

var errBotIncompatible = errors.New("当前运行中的 Caelis 服务与此版 Bot 协议不兼容。请选择支持 Bot 的版本；更新程序后，还需重启 Caelis 服务")

func initialize(ctx context.Context, c *client) (wire.ServerInfo, error) {
	var i wire.ServerInfo
	e := c.json(ctx, "GET", "/initialize", nil, &i, "", "")
	if e != nil {
		return i, e
	}
	if i.ProtocolVersion != 1 || i.ApiVersion != "v1" || i.EnvelopeVersion != "caelis.control.envelope/v1" || value(i.StoreId) == "" || value(i.InstanceId) == "" {
		return i, errBotIncompatible
	}
	for _, cap := range required {
		if !slices.Contains(i.Capabilities, cap) {
			return i, errBotIncompatible
		}
	}
	return i, nil
}
func Probe(ctx context.Context, settings api.RuntimeSettings) error {
	return ProbeBinding(ctx, settings, "")
}

// ProbeBinding validates the existing identity without registering or activating.
func ProbeBinding(ctx context.Context, settings api.RuntimeSettings, directory string) error {
	if _, e := caelisruntime.Find(settings.CLIPath); e != nil {
		return e
	}
	d, t, e := Discover(settings)
	if e != nil {
		return e
	}
	c, e := newClient(d.Endpoint, t)
	if e != nil {
		return e
	}
	i, e := initialize(ctx, c)
	if e == nil && value(i.InstanceId) != d.InstanceID {
		return errors.New("Caelis 服务已替换，请重新检测")
	}
	if e != nil || directory == "" {
		return e
	}
	b, e := loadBinding(filepath.Join(directory, "binding.json"))
	if e != nil {
		return e
	}
	if b.StoreID != "" && (b.StoreID != value(i.StoreId) || b.PrincipalID != d.PrincipalID) {
		return errors.New("所选 Caelis 数据目录与已有 Bot 身份不一致，原有连接保持不变")
	}
	return nil
}
func (s *Session) connect(ctx context.Context) error {
	d, hostToken, e := Discover(s.settings)
	if e != nil {
		return e
	}
	host, e := newClient(d.Endpoint, hostToken)
	if e != nil {
		return e
	}
	info, e := initialize(ctx, host)
	if e != nil {
		return e
	}
	if value(info.InstanceId) != d.InstanceID {
		return errors.New("Caelis 服务发现记录已过期")
	}
	s.mu.Lock()
	if s.state.StoreID != "" && (s.state.StoreID != value(info.StoreId) || s.state.PrincipalID != d.PrincipalID) {
		s.mu.Unlock()
		return errors.New("Caelis 授权存储或主体已改变，原有绑定已保留；不能自动接管新存储")
	}
	s.state.StoreID = value(info.StoreId)
	s.state.PrincipalID = d.PrincipalID
	s.state.Endpoint = d.Endpoint
	s.info = info
	e = s.saveLocked()
	s.mu.Unlock()
	if e != nil {
		return e
	}
	if s.state.Bot.Id == "" {
		if s.state.CreateID != "" {
			return errors.New("上次创建 Bot 的结果未确认，请核对原请求，不能自动再次创建")
		}
		op := "bot-create-" + rand.Text()
		s.mu.Lock()
		s.state.CreateID = op
		e = s.saveLocked()
		s.mu.Unlock()
		if e != nil {
			return e
		}
		req := wire.CreateBotRequest{OperationId: &op, Config: wire.BotConfig{Name: "Caelis Bot", ManagedWork: pointer(true), DesktopActions: pointer(true), WorkPermission: pointer("workspace-write")}}
		var res wire.CommandResult
		if e = host.json(ctx, "POST", "/bots/create", req, &res, op, ""); e != nil {
			s.clearRejectedEnrollment(e, true)
			return e
		}
		if !succeeded(res.Outcome) {
			if res.Outcome == "rejected" || res.Outcome == "conflicted" {
				s.mu.Lock()
				s.state.CreateID = ""
				_ = s.saveLocked()
				s.mu.Unlock()
			}
			return errors.New("Caelis 未接受 Bot 创建，请检查运行时的模型配置")
		}
		if res.Resource == nil || value(res.Resource.Ref) == "" {
			return errors.New("Caelis 未返回 Bot 绑定")
		}
		// The accepted identity must survive a lost subsequent read response.
		s.mu.Lock()
		s.state.Bot = wire.Bot{Id: value(res.Resource.Ref)}
		e = s.saveLocked()
		s.mu.Unlock()
		if e != nil {
			return e
		}
	}
	var vault struct{ StoreID, PrincipalID, ClientID, Token string }
	raw, e := privateRead(secretPath(s.path), 65536)
	if e == nil {
		if json.Unmarshal(raw, &vault) != nil || vault.StoreID != s.state.StoreID || vault.PrincipalID != s.state.PrincipalID || vault.Token == "" {
			return errors.New("Caelis 客户端凭据与绑定不一致")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	} else {
		if s.state.RegisterID != "" {
			return errors.New("客户端注册凭据未确认，不能自动重复注册；需要重新授权注册")
		}
		op := "client-register-" + rand.Text()
		s.mu.Lock()
		s.state.RegisterID = op
		e = s.saveLocked()
		s.mu.Unlock()
		if e != nil {
			return e
		}
		var reg wire.BotClientRegistration
		req := wire.RegisterBotClientRequest{BotId: s.state.Bot.Id, OperationId: &op, Actions: []string{"clock", "reminders", "gesture"}}
		if e = host.json(ctx, "POST", s.botPath("/clients/register"), req, &reg, op, ""); e != nil {
			s.clearRejectedEnrollment(e, false)
			return e
		}
		if reg.Token == "" || reg.Client.BotId != s.state.Bot.Id {
			return errors.New("Caelis 未返回有效客户端凭据")
		}
		vault.StoreID = s.state.StoreID
		vault.PrincipalID = s.state.PrincipalID
		vault.ClientID = reg.Client.Id
		vault.Token = reg.Token
		if e = privateWrite(secretPath(s.path), vault); e != nil {
			return errors.New("客户端凭据未能安全保存，已停止注册")
		}
	}
	scoped, e := newClient(d.Endpoint, vault.Token)
	if e != nil {
		return e
	}
	var life wire.BotClient
	if e = scoped.json(ctx, "GET", s.botPath("/client"), nil, &life, "", ""); e != nil {
		return e
	}
	if life.Id != vault.ClientID || life.BotId != s.state.Bot.Id || life.PrincipalId != s.state.PrincipalID {
		return errors.New("客户端主体不匹配")
	}
	if !life.Active || life.InstanceId != value(info.InstanceId) || !life.ExpiresAt.After(time.Now().Add(time.Minute)) {
		if e = scoped.json(ctx, "POST", s.botPath("/client/activate"), struct{}{}, &life, "", ""); e != nil {
			return e
		}
	}
	var bot wire.Bot
	if e = scoped.json(ctx, "GET", s.botPath(""), nil, &bot, "", ""); e != nil {
		return e
	}
	if !value(bot.Config.ManagedWork) || !value(bot.Config.DesktopActions) {
		return errors.New("此 Bot 尚未启用工作委派和桌面能力")
	}
	s.mu.Lock()
	s.generation++
	if s.streamCancel != nil {
		s.streamCancel()
	}
	s.streamCtx = nil
	s.streamCancel = nil
	s.streams = map[string]bool{}
	if s.state.InstanceID != value(info.InstanceId) {
		s.streams = map[string]bool{}
		for _, v := range s.state.Views {
			v.State.Approval = wire.ApprovalState{}
			v.State.Run = wire.RunState{}
		}
	}
	s.client = scoped
	s.state.InstanceID = value(info.InstanceId)
	s.state.Client = life
	s.state.Bot = bot
	e = s.saveLocked()
	s.mu.Unlock()
	return e
}
func (s *Session) botPath(suffix string) string { return "/bots/" + idPath(s.state.Bot.Id) + suffix }
func (s *Session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	s.connected = false
	s.bumpLocked()
	s.mu.Unlock()
	s.wg.Wait()
	s.step.Lock()
	defer s.step.Unlock()
	if s.client == nil || s.state.Client.ActivationId == "" {
		return nil
	}
	op := "exit-" + s.state.Client.ActivationId
	req := wire.BotClientExitRequest{BotId: s.state.Bot.Id, SessionId: pointer(s.state.Bot.SessionId), ActivationId: s.state.Client.ActivationId, CancelOwnedWork: pointer(true), OperationId: &op}
	_, e := s.command(ctx, op, s.botPath("/client/exit"), req)
	return e
}

func (s *Session) clearRejectedEnrollment(err error, create bool) {
	var remote *remoteError
	if !errors.As(err, &remote) || (remote.Status != 400 && remote.Status != 401 && remote.Status != 403 && remote.Status != 404) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if create {
		s.state.CreateID = ""
	} else {
		s.state.RegisterID = ""
	}
	_ = s.saveLocked()
}
