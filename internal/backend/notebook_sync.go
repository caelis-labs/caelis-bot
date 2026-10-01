package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
)

type NotebookSyncController interface {
	State() notebooksync.State
	Sync(context.Context, string) error
	Switch(context.Context, string) error
}

func (s *Service) ConfigureNotebookSync(c NotebookSyncController) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notebookSync = c
}
func (s *Service) notebookSyncController() (NotebookSyncController, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notebookSync == nil {
		return nil, errors.New("Notebook sync has not been prepared on this APP")
	}
	return s.notebookSync, nil
}
func (s *Service) NotebookSyncState() (notebooksync.State, error) {
	c, err := s.notebookSyncController()
	if err != nil {
		return notebooksync.State{}, err
	}
	return c.State(), nil
}
func (s *Service) SyncNotebook(ctx context.Context, nodeID string) (notebooksync.State, error) {
	c, err := s.notebookSyncController()
	if err != nil {
		return notebooksync.State{}, err
	}
	err = c.Sync(ctx, nodeID)
	return c.State(), err
}
func (s *Service) SwitchNotebookNode(ctx context.Context, nodeID string) (notebooksync.State, error) {
	c, err := s.notebookSyncController()
	if err != nil {
		return notebooksync.State{}, err
	}
	err = c.Switch(ctx, nodeID)
	return c.State(), err
}
