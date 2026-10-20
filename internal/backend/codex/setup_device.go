package codex

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"regexp"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const setupDeviceLifetime = 15 * time.Minute

var deviceUserCode = regexp.MustCompile(`^[A-Za-z0-9-]{4,32}$`)

func officialDeviceURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host == "auth.openai.com" && u.Path == "/codex/device" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func (s *Setup) finishDeviceLocked(state string) {
	if s.device == nil {
		return
	}
	s.device.State = state
	s.device.UserCode = ""
	s.device.VerificationURL = ""
	s.device.ExpiresAt = ""
	if s.deviceTimer != nil {
		s.deviceTimer.Stop()
		s.deviceTimer = nil
	}
}
func (s *Setup) deviceActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.device != nil && (s.device.State == "pending" || s.device.State == "unknown")
}
func (s *Setup) deviceViewLocked(ctx context.Context) *api.SetupLogin {
	if s.device == nil {
		return nil
	}
	if s.deviceOwner != api.SetupAccess(ctx) {
		if s.device.State == "pending" || s.device.State == "unknown" {
			return &api.SetupLogin{State: "other-session"}
		}
		return nil
	}
	v := *s.device
	if deadline, err := time.Parse(time.RFC3339, v.ExpiresAt); err == nil && !time.Now().Before(deadline) {
		// The timer may be waiting for a bounded management operation. Stop
		// displaying the challenge at the deadline even before cancellation returns.
		v.State, v.UserCode, v.VerificationURL = "unknown", "", ""
	}
	return &v
}

// Called with op held. The management client is CLIOnly: it has no conversation,
// Worker or shared-daemon ownership. Codex owns tokens and all issuer polling.
func (s *Setup) startDevice(ctx context.Context, c *Client) error {
	s.mu.Lock()
	if s.device != nil && (s.device.State == "pending" || s.device.State == "unknown") {
		same := s.deviceOwner == api.SetupAccess(ctx)
		s.mu.Unlock()
		if !same {
			return errors.New("请在发起登录的页面继续")
		}
		return nil // Read back the same attempt; never dispatch another native login.
	}
	s.mu.Unlock()
	auth, err := c.ReadAuthStatus(ctx)
	if err != nil {
		return errors.New("暂时无法确认已有登录，请重试")
	}
	if auth.AccountPresent || !auth.RequiresOpenAIAuth {
		return nil
	} // Never replace existing credentials.
	s.mu.Lock()
	id := rand.Text()
	s.deviceOwner = api.SetupAccess(ctx)
	s.device = &api.SetupLogin{ID: id, State: "unknown", ExpiresAt: time.Now().Add(setupDeviceLifetime).UTC().Format(time.RFC3339)}
	s.completed = map[string]bool{}
	epoch := s.epoch
	s.deviceTimer = time.AfterFunc(setupDeviceLifetime, func() { s.expireDevice(epoch, id) })
	s.mu.Unlock()
	var out struct {
		Type            string `json:"type"`
		LoginID         string `json:"loginId"`
		VerificationURL string `json:"verificationUrl"`
		UserCode        string `json:"userCode"`
	}
	err = callDecode(ctx, c, "account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &out)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		var native *NativeError
		if errors.As(err, &native) {
			state := "unavailable"
			if native.Code == -32601 || native.Code == -32602 {
				state = "unsupported"
			}
			s.finishDeviceLocked(state)
		}
		// Transport uncertainty is deliberately retained. Explicit cancellation stops
		// this dedicated management client if no native loginId was received.
		return nil
	}
	s.loginID = out.LoginID
	if out.Type != "chatgptDeviceCode" || out.LoginID == "" || !officialDeviceURL(out.VerificationURL) || !deviceUserCode.MatchString(out.UserCode) {
		return nil // Unknown/malformed response must not authorize a new start.
	}
	s.device.State = "pending"
	s.device.UserCode = out.UserCode
	s.device.VerificationURL = out.VerificationURL
	s.notice = ""
	if success, ok := s.completed[out.LoginID]; ok {
		if success {
			s.finishDeviceLocked("complete")
		} else {
			s.finishDeviceLocked("failed")
		}
		s.loginID = ""
		delete(s.completed, out.LoginID)
	}
	return nil
}

func (s *Setup) cancelDevice(ctx context.Context, c *Client, expected, state string) error {
	s.mu.Lock()
	if s.device == nil {
		s.mu.Unlock()
		return nil
	}
	if s.deviceOwner != api.SetupAccess(ctx) || expected == "" || expected != s.device.ID {
		s.mu.Unlock()
		return errors.New("登录已变化，请刷新后重试")
	}
	id := s.loginID
	active := s.device.State == "pending" || s.device.State == "unknown"
	s.mu.Unlock()
	if !active {
		return nil
	}
	if id == "" {
		// The original start response was lost. There is no safe second start or
		// guessed cancellation ID. Reap only this owned, setup-only process.
		c.Close()
		s.client = nil
	} else {
		var out struct {
			Status string `json:"status"`
		}
		if err := callDecode(ctx, c, "account/login/cancel", map[string]string{"loginId": id}, &out); err != nil || (out.Status != "canceled" && out.Status != "notFound") {
			return errors.New("取消结果尚未确认，请重试取消")
		}
	}
	s.mu.Lock()
	s.loginID = ""
	s.loginURL = ""
	s.finishDeviceLocked(state)
	s.mu.Unlock()
	return nil
}
func (s *Setup) expireDevice(epoch uint64, id string) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	if s.closed || s.epoch != epoch || s.device == nil || s.device.ID != id || (s.device.State != "pending" && s.device.State != "unknown") {
		s.mu.Unlock()
		return
	}
	owner := s.deviceOwner
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(api.WithSetupAccess(context.Background(), owner), 2*time.Second)
	defer cancel()
	if s.client != nil {
		if err := s.cancelDevice(ctx, s.client, id, "expired"); err != nil {
			s.client.Close()
			s.client = nil // Bounded expiry: only the setup owner is stopped.
		}
	}
	s.mu.Lock()
	if s.device.State == "pending" || s.device.State == "unknown" {
		s.loginID = ""
		s.finishDeviceLocked("expired")
	}
	s.mu.Unlock()
}
