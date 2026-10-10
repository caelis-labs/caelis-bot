package desktop

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/weixin"
)

func (s *Service) WeixinStatus() weixin.Status {
	if s.weixin == nil {
		return weixin.Status{Issue: "unavailable"}
	}
	return s.weixin.Status()
}
func (s *Service) StartWeixinPairing() (weixin.Status, error) {
	if s.weixin == nil {
		return s.WeixinStatus(), errors.New("unavailable")
	}
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	return s.weixin.StartPairing(ctx)
}
func (s *Service) VerifyWeixinPairing(code string) (weixin.Status, error) {
	if s.weixin == nil {
		return s.WeixinStatus(), errors.New("unavailable")
	}
	return s.weixin.Verify(code)
}
func (s *Service) ConfirmWeixinPairing() (weixin.Status, error) {
	if s.weixin == nil {
		return s.WeixinStatus(), errors.New("unavailable")
	}
	return s.weixin.Confirm()
}
func (s *Service) PauseWeixin() (weixin.Status, error) {
	if s.weixin == nil {
		return s.WeixinStatus(), errors.New("unavailable")
	}
	return s.weixin.Pause()
}
func (s *Service) ResumeWeixin() (weixin.Status, error) {
	if s.weixin == nil {
		return s.WeixinStatus(), errors.New("unavailable")
	}
	return s.weixin.Resume()
}
func (s *Service) ForgetWeixin() (weixin.Status, error) {
	if s.weixin == nil {
		return s.WeixinStatus(), errors.New("unavailable")
	}
	return s.weixin.Forget()
}
