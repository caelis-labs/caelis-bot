package desktop

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/telegram"
)

func (s *Service) TelegramStatus() telegram.Status {
	if s.telegram == nil {
		return telegram.Status{Issue: "unavailable"}
	}
	return s.telegram.Status()
}
func (s *Service) ConnectTelegram(token string, takeOver bool) (telegram.Status, error) {
	if s.telegram == nil {
		return s.TelegramStatus(), errors.New("unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.telegram.Connect(ctx, token, takeOver)
}
func (s *Service) ConfirmTelegram() (telegram.Status, error) {
	if s.telegram == nil {
		return s.TelegramStatus(), errors.New("unavailable")
	}
	return s.telegram.Confirm()
}
func (s *Service) DisconnectTelegram() (telegram.Status, error) {
	if s.telegram == nil {
		return s.TelegramStatus(), errors.New("unavailable")
	}
	return s.telegram.Disconnect()
}
func (s *Service) ForgetTelegram() (telegram.Status, error) {
	if s.telegram == nil {
		return s.TelegramStatus(), errors.New("unavailable")
	}
	return s.telegram.Forget()
}
func (s *Service) OpenTelegramSetup(pair bool) error {
	url := "https://t.me/BotFather"
	if pair {
		status := s.TelegramStatus()
		url = status.PairURL
		if url == "" && status.Bot != "" {
			url = "https://t.me/" + status.Bot
		}
		if url == "" {
			return errors.New("pairing_expired")
		}
	}
	// Native app links go through the existing bounded opener, without a token.
	if s.openExternalURL == nil {
		return errors.New("unavailable")
	}
	return s.openExternalURL(url)
}
