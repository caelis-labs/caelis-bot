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
