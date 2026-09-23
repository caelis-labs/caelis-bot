package desktop

import (
	"encoding/json"
	"errors"
	"math"
)

// DesktopContext is a presentation snapshot, never evidence of Agent task ownership.
// Native hosts may leave window geometry unknown. Coordinates are logical desktop points.
func (s *Service) DesktopContext() json.RawMessage {
	<-s.ready
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(contextDriver); ok && !s.stopped {
		return d.desktopContext()
	}
	return json.RawMessage("null")
}

func (s *Service) PlaneReady(ready bool) {
	<-s.ready
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(propDriver); ok && !s.stopped {
		d.planeReady(ready)
	}
}

// A renderer requests one bounded local prop flight; it never owns an OS window.
// Release points use the actor's 180 x 240 local coordinates, bottom-up.
func (s *Service) LaunchPlane(id string, x, y float64) (bool, error) {
	if len(id) == 0 || len(id) > 64 || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || x < 0 || x > 180 || y < 0 || y > 240 {
		return false, errors.New("invalid prop release")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(propDriver); ok && s.started && !s.stopped && s.placement.Visible && s.characterActivity != "working" && s.characterActivity != "waiting" {
		return d.launchPlane(id, x, y), nil
	}
	return false, nil
}

func (s *Service) FinishPlane(id string, completed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(propDriver); ok && !s.stopped {
		d.finishPlane(id, completed)
	}
}
