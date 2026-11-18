package codex

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Setup subscribes to the public daemon on its own connection for login notifications.
// op serializes UI operations; notifications never block a synchronous RPC.
type Setup struct {
	op                 sync.Mutex
	mu                 sync.Mutex
	client             *Client
	binary, socket     string
	loginID            string
	device             *api.SetupLogin
	deviceOwner        string
	deviceTimer        *time.Timer
	completed          map[string]bool
	closed             bool
	epoch, deviceEpoch uint64
	workers            sync.WaitGroup
	start              func(context.Context, Options) (*Client, error)
}

func (s *Setup) clientFor(ctx context.Context, path string) (*Client, error) {
	if s.closed {
		return nil, errors.New("运行时管理已关闭")
	}
	socket := controlSocket("")
	if socket == "" {
		return nil, errExistingServer
	}
	if s.socket != "" && s.socket != socket && s.deviceActive() {
		return nil, errors.New("请先结束当前登录")
	}
	if s.client != nil {
		select {
		case <-s.client.Done():
		default:
			if s.socket == socket {
				s.binary = path
				return s.client, nil
			}
		}
		s.client.Close()
		s.client = nil
	}
	c, e := openShared(ctx, path, socket, s.start)
	if e != nil {
		return nil, e
	}
	s.client, s.binary, s.socket = c, path, socket
	s.mu.Lock()
	s.epoch++
	epoch := s.epoch
	s.mu.Unlock()
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		for n := range c.Notifications() {
			if n.Method != "account/login/completed" {
				continue
			}
			var v struct {
				LoginID string `json:"loginId"`
				Success bool   `json:"success"`
			}
			if json.Unmarshal(n.Params, &v) != nil || v.LoginID == "" {
				continue
			}
			s.mu.Lock()
			if s.epoch == epoch {
				s.deviceCompletedLocked(v.LoginID, v.Success)
			}
			s.mu.Unlock()
		}
	}()
	return s.client, nil
}
func (s *Setup) Inspect(ctx context.Context, path string) (api.SetupState, error) {
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	v := api.SetupState{State: "login", Models: []api.SetupChoice{}}
	c, e := s.clientFor(ctx, path)
	if e != nil {
		v.State = "unavailable"
		if incompatibleProtocol(e) {
			v.State = "incompatible"
		}
		return v, nil
	}
	auth, e := c.ReadAuthStatus(ctx)
	if e != nil {
		return v, errors.New("无法读取 Codex 登录状态，请重新检测")
	}
	s.mu.Lock()
	if auth.AccountPresent && s.device != nil && (s.device.State == "pending" && s.epoch != s.deviceEpoch || s.device.State == "unknown") {
		// Account readiness survives a missed completion notification. It
		// establishes the current account without attributing a login receipt.
		s.finishDeviceLocked("unconfirmed")
	}
	v.Login = s.deviceViewLocked(ctx)
	v.LoginPending = s.loginID != "" || s.device != nil && s.device.State == "unknown"
	s.mu.Unlock()
	v.AccountType = auth.AccountType
	if v.LoginPending {
		v.Message = "请在浏览器中完成登录"
		return v, nil
	}
	if !auth.AccountPresent && auth.RequiresOpenAIAuth {
		return v, nil
	}
	models, e := c.models(ctx)
	if e != nil {
		return v, e
	}
	for _, m := range models {
		v.Models = append(v.Models, api.SetupChoice{Value: m.Model, Label: m.Name, Current: m.Default, Parameters: &m})
	}
	v.State = "ready"
	return v, nil
}

// Apply owns the existing standard management client. Device auth never opens a host browser.
func (s *Setup) Apply(ctx context.Context, path, action, key string, attempt ...string) (string, error) {
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, e := s.clientFor(ctx, path)
	if e != nil {
		return "", errors.New("请先检测 Codex 连接")
	}
	switch action {
	case "login":
		return "", s.startDevice(ctx, c)
	case "cancel-login":
		expected := ""
		if len(attempt) > 0 {
			expected = attempt[0]
		}
		return "", s.cancelDevice(ctx, c, expected, "cancelled")
	case "api-key":
		auth, err := c.ReadAuthStatus(ctx)
		if err != nil {
			return "", errors.New("暂时无法确认已有登录")
		}
		if auth.AccountPresent {
			return "", errors.New("已有登录，未替换账号")
		}
		if key == "" || len(key) > 16384 {
			return "", errors.New("请输入有效的 API Key")
		}
		if s.deviceActive() {
			return "", errors.New("请先取消当前登录")
		}
		if e = callDecode(ctx, c, "account/login/start", map[string]string{"type": "apiKey", "apiKey": key}, nil); e != nil {
			return "", errors.New("登录结果未确认，请重新检测；密钥未保存在 Bot 中")
		}
	case "logout":
		if s.deviceActive() {
			return "", errors.New("请先取消当前登录")
		}
		if e = callDecode(ctx, c, "account/logout", nil, nil); e != nil {
			return "", errors.New("退出登录结果未确认，请重新检测")
		}
	default:
		return "", errors.New("不支持的登录操作")
	}
	return "", nil
}

// PreparedHome identifies the live management client for short-lived setup
// snapshots. It exposes no credentials and never starts or authorizes a client.
func (s *Setup) PreparedHome(path string) (string, bool) {
	s.op.Lock()
	defer s.op.Unlock()
	if s.closed || s.client == nil || s.binary != path || s.socket != controlSocket("") {
		return "", false
	}
	select {
	case <-s.client.Done():
		return "", false
	default:
	}
	return s.client.home, true
}
func (s *Setup) Close() {
	s.op.Lock()
	defer s.op.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.mu.Lock()
	id := s.loginID
	s.finishDeviceLocked("interrupted")
	s.mu.Unlock()
	if s.client != nil {
		if id != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = callDecode(ctx, s.client, "account/login/cancel", map[string]string{"loginId": id}, nil)
			cancel()
		}
		s.client.Close()
	}
	s.workers.Wait()
}
