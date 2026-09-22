package codex

import (
	"context"
	"errors"
	"time"
)

func (s *Session) Login(ctx context.Context) (string, error) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	c := s.client
	allowed := s.state.Connection == "login" && s.loginID == ""
	if allowed {
		s.loginStarting = true
		s.earlyLogin = map[string]bool{}
	}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.loginStarting = false; s.earlyLogin = nil; s.mu.Unlock() }()
	if c == nil || !allowed {
		return "", errors.New("请先连接，或等待当前登录结束")
	}
	ctx, cancel := s.operation(ctx, 30*time.Second)
	defer cancel()
	var response struct {
		Type    string `json:"type"`
		LoginID string `json:"loginId"`
		AuthURL string `json:"authUrl"`
	}
	if callDecode(ctx, c, "account/login/start", map[string]string{"type": "chatgpt"}, &response) != nil || response.Type != "chatgpt" || response.LoginID == "" || !safeWebURL(response.AuthURL) {
		return "", errors.New("无法发起登录，请重试")
	}
	s.mu.Lock()
	s.loginID = response.LoginID
	s.state.LoginPending = true
	s.state.Message = "请在浏览器中完成登录"
	if success, ok := s.earlyLogin[response.LoginID]; ok {
		s.loginCompleted(success)
	}
	s.update()
	s.mu.Unlock()
	return response.AuthURL, nil
}
func (s *Session) CancelLogin(ctx context.Context) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	c, id := s.client, s.loginID
	s.mu.Unlock()
	if c == nil || id == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if callDecode(ctx, c, "account/login/cancel", map[string]string{"loginId": id}, nil) != nil {
		return errors.New("暂时无法取消登录")
	}
	s.mu.Lock()
	s.loginID = ""
	s.state.LoginPending = false
	s.state.Message = "登录已取消"
	s.update()
	s.mu.Unlock()
	return nil
}

// Called under mu, for both an early notification and the normal event path.
func (s *Session) loginCompleted(success bool) {
	s.loginID = ""
	s.state.LoginPending = false
	if success {
		s.state.Message = "登录完成，正在恢复连接…"
		go s.completeLogin(s.epoch)
	} else {
		s.state.Message = "登录未完成，请重试"
	}
}
