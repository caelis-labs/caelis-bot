package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type NotebookBackupTarget struct {
	NodeID  string          `json:"nodeId"`
	Backend api.NodeBackend `json:"backend"`
}
type NotebookSyncSettings struct {
	Enabled         bool                   `json:"enabled"`
	SourceBackend   api.NodeBackend        `json:"sourceBackend,omitempty"`
	SourceNodeID    string                 `json:"sourceNodeId"`
	IntervalMinutes int                    `json:"intervalMinutes"`
	Targets         []NotebookBackupTarget `json:"targets"`
}
type notebookSettingsController interface {
	Settings() NotebookSyncSettings
	SaveSettings(context.Context, NotebookSyncSettings) (NotebookSyncSettings, error)
}

func (s *Service) NotebookSyncSettings() (NotebookSyncSettings, error) {
	c, e := s.notebookSyncController()
	if e != nil {
		return NotebookSyncSettings{}, e
	}
	settings, ok := c.(notebookSettingsController)
	if !ok {
		return NotebookSyncSettings{}, errors.New("Notebook settings are native assembly only")
	}
	return settings.Settings(), nil
}
func (s *Service) SaveNotebookSyncSettings(ctx context.Context, in NotebookSyncSettings) (NotebookSyncSettings, error) {
	c, e := s.notebookSyncController()
	if e != nil {
		return NotebookSyncSettings{}, e
	}
	settings, ok := c.(notebookSettingsController)
	if !ok {
		return NotebookSyncSettings{}, errors.New("Notebook settings are native assembly only")
	}
	return settings.SaveSettings(ctx, in)
}
