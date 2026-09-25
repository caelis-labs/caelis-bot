package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"slices"
	"time"
)

type taskDriver interface {
	tasks(string)
	taskFailure(string)
	taskOpening(string, string)
}

func (s *Service) observeTasks(tasks []api.TaskPreview) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	data, _ := json.Marshal(tasks)
	if string(data) == s.taskPreviewJSON {
		return
	}
	s.taskPreviewJSON = string(data)
	s.taskPreviews = slices.Clone(tasks)
	if d, ok := s.native.(taskDriver); ok {
		d.tasks(string(data))
	}
}

// Explicit clicks are the only terminal-launch path. Visibility and hover have
// no backend effect. Coalesce overlapping launches; the native button also
// collapses immediately so a second click cannot launch another task.
func (s *Service) openTask(ctx context.Context, id string) error {
	if !s.taskOpenMu.TryLock() {
		return nil
	}
	defer s.taskOpenMu.Unlock()
	s.mu.Lock()
	allowed := s.started && !s.stopped && slices.ContainsFunc(s.taskPreviews, func(t api.TaskPreview) bool { return t.ID == id })
	resolve, launch := s.resolveTaskTerminal, s.launchTaskTerminal
	s.mu.Unlock()
	if !allowed || resolve == nil || launch == nil {
		return errors.New("该任务暂不可用")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return context.Canceled
	}
	s.taskOpenCancel = cancel
	if d, ok := s.native.(taskDriver); ok {
		d.taskOpening(id, s.text("native.taskTerminalWaiting", nil))
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.taskOpenCancel = nil
	}()
	resolveCtx, resolveCancel := context.WithTimeout(ctx, 10*time.Second)
	target, err := resolve(resolveCtx, id)
	resolveCancel()
	if err == nil {
		err = launch(ctx, id, target)
	}
	s.mu.Lock()
	if d, ok := s.native.(taskDriver); ok && !s.stopped {
		d.taskOpening("", "")
	}
	s.mu.Unlock()
	if err != nil {
		if s.taskError != nil {
			s.taskError(id, err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if d, ok := s.native.(taskDriver); ok && !s.stopped {
			key := "native.taskTerminalFailed"
			if errors.Is(err, taskterminal.ErrUnconfirmed) {
				key = "native.taskTerminalUnconfirmed"
			}
			if errors.Is(err, context.Canceled) {
				key = "native.taskTerminalCanceled"
			}
			if errors.Is(err, taskterminal.ErrUnsupportedDefault) {
				key = "native.defaultTerminalUnsupported"
			}
			d.taskFailure(s.text(key, nil))
		}
	}
	return err
}

func (s *Service) cancelTerminalOpening() {
	s.mu.Lock()
	cancel := s.taskOpenCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
