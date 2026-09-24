package desktop

import (
	"errors"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/contentpack"
)

func (s *Service) ContentState() (contentpack.State, error) {
	if s.content == nil {
		return contentpack.State{}, errors.New(s.text("native.contentStorageUnavailable", nil))
	}
	return s.content.State(), nil
}
func (s *Service) Appearance() (contentpack.Appearance, error) {
	state, e := s.ContentState()
	return state.Appearance, e
}
func (s *Service) publishContent(state contentpack.State, err error) (contentpack.State, error) {
	if err == nil && s.contentChanged != nil {
		s.contentChanged(state.Appearance)
	}
	return state, err
}
func (s *Service) SelectAppearance(character, avatar string) (contentpack.State, error) {
	if s.content == nil {
		return s.ContentState()
	}
	return s.publishContent(s.content.Select(contentpack.Selection{Character: character, Avatar: avatar}))
}
func (s *Service) FallbackAppearance(revision uint64) (contentpack.Appearance, error) {
	if s.content == nil {
		return s.Appearance()
	}
	state, e := s.publishContent(s.content.Fallback(revision))
	return state.Appearance, e
}
func (s *Service) ImportContent() (contentpack.State, error) {
	if s.content == nil {
		return s.ContentState()
	}
	if !s.contentImportMu.TryLock() {
		return s.content.State(), errors.New(s.text("native.importInProgress", nil))
	}
	defer s.contentImportMu.Unlock()
	if s.pickContentFile == nil {
		return s.content.State(), errors.New(s.text("native.fileSelectionUnavailable", nil))
	}
	name, e := s.pickContentFile()
	if e != nil {
		return s.content.State(), e
	}
	if name == "" {
		return s.content.State(), nil
	}
	state, e := s.content.Install(name)
	if e != nil {
		return state, fmt.Errorf("%s", s.text("native.importFailed", map[string]any{"error": e.Error()}))
	}
	return s.publishContent(state, nil)
}
func (s *Service) RemoveContent(id string) (contentpack.State, error) {
	if s.content == nil {
		return s.ContentState()
	}
	return s.publishContent(s.content.Remove(id))
}
