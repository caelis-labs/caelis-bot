package codex

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Setup owns a separate standard client, without conversation or Bot authority.
// op serializes UI operations; notifications never block a synchronous RPC.
type Setup struct {
	op                        sync.Mutex
	mu                        sync.Mutex
	client                    *Client
	binary                    string
	loginID, loginURL, notice string
	completed                 map[string]bool
	closed                    bool
	epoch                     uint64
	workers                   sync.WaitGroup
	start                     func(context.Context, Options) (*Client, error)
}

func (s *Setup) clientFor(ctx context.Context, path string) (*Client, error) {
	if s.closed {
		return nil, errors.New("运行时管理已关闭")
	}
	if s.client != nil {
		select {
		case <-s.client.Done():
			s.client.Close()
			s.client = nil
		default:
		}
	}
	if s.client != nil && s.binary != path {
		s.client.Close()
		s.client = nil
	}
	if s.client == nil {
		start := s.start
		if start == nil {
			start = Start
		}
		c, e := start(ctx, Options{Binary: path, CLIOnly: true})
		if e != nil {
			return nil, e
		}
		s.client = c
		s.binary = path
		s.mu.Lock()
		s.loginID = ""
		s.loginURL = ""
		s.notice = ""
		s.completed = map[string]bool{}
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
				if s.epoch == epoch && len(s.completed) < 32 {
					s.completed[v.LoginID] = v.Success
				}
				s.mu.Unlock()
			}
		}()
	}
	return s.client, nil
}
func (s *Setup) Inspect(ctx context.Context, path string) (api.SetupState, error) {
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.inspect(ctx, path)
}
func (s *Setup) inspect(ctx context.Context, path string) (api.SetupState, error) {
	v := api.SetupState{State: "login", Models: []api.SetupChoice{}}
	c, e := s.clientFor(ctx, path)
	if e != nil {
		v.State = "incompatible"
		v.Message = "Codex 连接未通过，请更新或选择其他程序"
		return v, nil
	}
	s.mu.Lock()
	if success, ok := s.completed[s.loginID]; s.loginID != "" && ok {
		delete(s.completed, s.loginID)
		s.loginID = ""
		s.loginURL = ""
		if !success {
			s.notice = "登录未完成，请重试"
		}
	}
	v.LoginPending = s.loginID != ""
	v.Message = s.notice
	s.mu.Unlock()
	auth, e := c.ReadAuthStatus(ctx)
	if e != nil {
		return v, errors.New("无法读取 Codex 登录状态，请重新检测")
	}
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
		v.Models = append(v.Models, api.SetupChoice{Value: m.Model, Label: m.Name, Current: m.Default})
	}
	v.State = "ready"
	return v, nil
}

// Apply returns an official authorization URL for the native host to open.
func (s *Setup) Apply(ctx context.Context, path, action, key string) (string, error) {
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, e := s.clientFor(ctx, path)
	if e != nil {
		return "", errors.New("请先检测 Codex 连接")
	}
	s.mu.Lock()
	id, authURL := s.loginID, s.loginURL
	s.mu.Unlock()
	switch action {
	case "login":
		if id != "" {
			return authURL, nil
		}
		var out struct {
			Type    string `json:"type"`
			LoginID string `json:"loginId"`
			AuthURL string `json:"authUrl"`
		}
		if e = callDecode(ctx, c, "account/login/start", map[string]string{"type": "chatgpt"}, &out); e != nil || out.Type != "chatgpt" || out.LoginID == "" || !safeWebURL(out.AuthURL) {
			return "", errors.New("无法发起登录，请重新检测后重试")
		}
		s.mu.Lock()
		s.loginID = out.LoginID
		s.loginURL = out.AuthURL
		s.notice = ""
		s.mu.Unlock()
		return out.AuthURL, nil
	case "cancel-login":
		if id != "" {
			if e = callDecode(ctx, c, "account/login/cancel", map[string]string{"loginId": id}, nil); e != nil {
				return "", errors.New("取消结果未确认，请重新检测")
			}
		}
		s.mu.Lock()
		s.loginID = ""
		s.loginURL = ""
		s.notice = "登录已取消"
		s.mu.Unlock()
	case "api-key":
		if key == "" || len(key) > 16384 {
			return "", errors.New("请输入有效的 API Key")
		}
		if id != "" {
			return "", errors.New("请先取消浏览器登录")
		}
		if e = callDecode(ctx, c, "account/login/start", map[string]string{"type": "apiKey", "apiKey": key}, nil); e != nil {
			return "", errors.New("登录结果未确认，请重新检测；密钥未保存在 Bot 中")
		}
	case "logout":
		if id != "" {
			return "", errors.New("请先取消浏览器登录")
		}
		if e = callDecode(ctx, c, "account/logout", nil, nil); e != nil {
			return "", errors.New("退出登录结果未确认，请重新检测")
		}
	default:
		return "", errors.New("不支持的登录操作")
	}
	return "", nil
}
func (s *Setup) Models(ctx context.Context, path string) ([]api.ModelOption, error) {
	s.op.Lock()
	defer s.op.Unlock()
	c, e := s.clientFor(ctx, path)
	if e != nil {
		return nil, e
	}
	return c.models(ctx)
}
func (s *Setup) Close() {
	s.op.Lock()
	defer s.op.Unlock()
	s.closed = true
	if s.client != nil {
		s.mu.Lock()
		id := s.loginID
		s.mu.Unlock()
		if id != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = callDecode(ctx, s.client, "account/login/cancel", map[string]string{"loginId": id}, nil)
			cancel()
		}
		s.client.Close()
	}
	s.workers.Wait()
}
