package desktop

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

func (s *Service) Plugins() (plugins.Snapshot, error) {
	if s.pluginSnapshot == nil {
		return plugins.Snapshot{}, errors.New("plugin store unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := s.pluginSnapshot(ctx)
	return snapshot.Public(), err
}
func (s *Service) PluginAction(id, action string) (plugins.Snapshot, error) {
	if s.pluginAction == nil {
		return plugins.Snapshot{}, errors.New("plugin store unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshot, err := s.pluginAction(ctx, id, action)
	return snapshot.Public(), err
}
func (s *Service) PluginConnection(id, secret, caPEM string, clear bool) (plugins.Snapshot, error) {
	if s.pluginConnection == nil {
		return plugins.Snapshot{}, errors.New("plugin connection unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshot, err := s.pluginConnection(ctx, id, secret, caPEM, clear)
	return snapshot.Public(), err
}

func (s *Service) PluginOAuthStart(id string) (plugins.Snapshot, error) {
	if s.pluginOAuthStart == nil {
		return plugins.Snapshot{}, errors.New("OAuth connection unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshot, err := s.pluginOAuthStart(ctx, id)
	return snapshot.Public(), err
}
func (s *Service) PluginOAuthCancel(id string) (plugins.Snapshot, error) {
	if s.pluginOAuthCancel == nil {
		return plugins.Snapshot{}, errors.New("OAuth connection unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := s.pluginOAuthCancel(ctx, id)
	return snapshot.Public(), err
}

func (s *Service) PluginSkillDetail(id, skill string) (plugins.SkillDetail, error) {
	if s.pluginSkillDetail == nil {
		return plugins.SkillDetail{}, errors.New("plugin store unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.pluginSkillDetail(ctx, id, skill)
}

func (s *Service) PluginServerDetail(id, server string, refresh bool) (plugins.ServerDetail, error) {
	if s.pluginServerDetail == nil {
		return plugins.ServerDetail{}, errors.New("plugin store unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return s.pluginServerDetail(ctx, id, server, refresh)
}
