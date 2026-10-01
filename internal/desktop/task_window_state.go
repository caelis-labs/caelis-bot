package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"log"
	"slices"
)

// Platform-specific operations such as placement remain in the native adapter.
type taskWindowController interface {
	Click(context.Context, string) error
	Dismiss(context.Context, string) error
	CancelAll()
}

func (s *Service) taskWindowAllowed(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started && !s.stopped && slices.ContainsFunc(s.taskPreviews, func(t api.TaskPreview) bool { return t.ID == id })
}

// Projection only: callbacks cannot issue window mutations. Sequence fencing
// also covers an old completion arriving after a newer click or a new binding.
func (s *Service) taskWindowChanged(id string, event taskterminal.WindowEvent) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if s.taskWindowEvents == nil {
		s.taskWindowEvents = map[string]taskterminal.WindowEvent{}
	}
	if previous, ok := s.taskWindowEvents[id]; ok && previous.Revision >= event.Revision {
		s.mu.Unlock()
		return
	}
	s.taskWindowEvents[id] = event
	outcome := "pending"
	if event.Phase == taskterminal.WindowIdle || event.Phase == taskterminal.WindowUncertain {
		outcome = "confirmed"
		switch {
		case errors.Is(event.Err, taskterminal.ErrWindowSuperseded):
			outcome = "superseded"
		case errors.Is(event.Err, context.Canceled):
			outcome = "cancelled"
		case errors.Is(event.Err, taskterminal.ErrWindowCloseCancelled):
			outcome = "cancelled"
		case errors.Is(event.Err, taskterminal.ErrWindowOpenCancelled):
			outcome = "cancelled"
		case errors.Is(event.Err, taskterminal.ErrWindowNotConnected):
			outcome = "unconfirmed"
		case errors.Is(event.Err, taskterminal.ErrWindowClosePending):
			outcome = "pending"
		case errors.Is(event.Err, taskterminal.ErrWindowUnsettled):
			outcome = "unconfirmed"
		case event.Err != nil:
			outcome = "rejected"
		}
	}
	log.Printf("Task terminal transition: sequence=%d phase=%s observed=%s outcome=%s", event.Revision, event.Phase, event.State, outcome)
	busy := map[string]taskterminal.WindowTransition{}
	opening := ""
	closing := false
	for key, e := range s.taskWindowEvents {
		switch e.Phase {
		case taskterminal.WindowOpening, taskterminal.WindowClosing:
			busy[key] = e.Phase
			if opening == "" || key < opening {
				opening = key
				closing = e.Phase == taskterminal.WindowClosing
			}
		case taskterminal.WindowShowing, taskterminal.WindowCollapsing, taskterminal.WindowReconciling:
			busy[key] = e.Phase
		}
	}
	if d, ok := s.native.(interface{ taskTransitions(string) }); ok {
		data, _ := json.Marshal(busy)
		d.taskTransitions(string(data))
	}
	if d, ok := s.native.(taskDriver); ok {
		promptKey := opening
		if closing {
			promptKey += "/closing"
		}
		if s.taskWindowPrompt != promptKey {
			message := ""
			if opening != "" {
				message = s.text("native.taskTerminalWaiting", nil)
				if closing {
					message = s.text("native.taskTerminalClosing", nil)
				}
			}
			d.taskOpening(opening, message)
			s.taskWindowPrompt = promptKey
		}
		if event.Phase == taskterminal.WindowReconciling && opening == "" {
			d.taskOpening(opening, "")
		}
		if event.Err != nil && event.Phase == taskterminal.WindowUncertain {
			d.taskFailure(s.text(taskWindowErrorKey(event.Err), nil))
		}
		if event.Phase == taskterminal.WindowIdle && errors.Is(event.Err, taskterminal.ErrWindowNotConnected) {
			d.taskFailure(s.text("native.taskTerminalNotConnected", nil))
		}
	}
	observe := s.observeTaskTerminal
	report := s.taskError
	s.mu.Unlock()
	if event.Err != nil && event.Phase == taskterminal.WindowUncertain && report != nil {
		report(id, event.Err)
	}
	if event.Phase == taskterminal.WindowIdle && event.Err == nil && event.State == taskterminal.WindowForeground && observe != nil {
		go observe(context.Background(), id)
	}
}
func taskWindowErrorKey(err error) string {
	switch {
	case errors.Is(err, api.ErrRemoteWorkTerminal):
		return "host.remoteTaskTerminalUnavailable"
	case errors.Is(err, api.ErrWorkTerminalOffline):
		return "host.taskTerminalNodeUnavailable"
	case errors.Is(err, api.ErrWorkTerminalBinding):
		return "host.taskTerminalBindingChanged"
	case errors.Is(err, taskterminal.ErrWindowClosePending):
		return "native.taskTerminalClosePending"
	case errors.Is(err, taskterminal.ErrUnconfirmed):
		return "native.taskTerminalUnconfirmed"
	case errors.Is(err, taskterminal.ErrUnsupportedDefault):
		return "native.defaultTerminalUnsupported"
	case errors.Is(err, taskterminal.ErrWindowPermission):
		return "native.taskTerminalPermission"
	case errors.Is(err, taskterminal.ErrWindowUnsupported):
		return "native.taskTerminalUnsupported"
	case errors.Is(err, taskterminal.ErrWindowIdentity):
		return "native.taskTerminalIdentity"
	case errors.Is(err, taskterminal.ErrWindowUnsettled):
		return "native.taskTerminalUnsettled"
	default:
		return "native.taskTerminalControl"
	}
}
