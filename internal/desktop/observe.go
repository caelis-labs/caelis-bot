package desktop

import (
	"context"
	"errors"
	"os"

	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
)

type observationDriver interface {
	observeDesktop(context.Context) (desktopcontrol.Frame, error)
}

func (s *Service) desktopExperiment() desktopcontrol.Observer {
	if os.Getenv("CAELIS_BOT_DESKTOP_POC") != "1" {
		return nil
	}
	return func(ctx context.Context) (desktopcontrol.Frame, error) {
		select {
		case <-ctx.Done():
			return desktopcontrol.Frame{}, ctx.Err()
		case <-s.ready:
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if driver, ok := s.native.(observationDriver); ok && !s.stopped {
			frame, err := driver.observeDesktop(ctx)
			frame.World.ActorVisible = s.placement.Visible
			return frame, err
		}
		return desktopcontrol.Frame{}, errors.New("desktop observation unavailable on this host")
	}
}
