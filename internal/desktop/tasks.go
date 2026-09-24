package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"slices"
	"time"
)

type taskDriver interface {
	tasks(string)
	taskFailure(string)
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	target, err := resolve(ctx, id)
	if err == nil {
		err = launch(ctx, id, target)
	}
	if err != nil {
		if s.taskError != nil {
			s.taskError(id, err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if d, ok := s.native.(taskDriver); ok && !s.stopped {
			d.taskFailure("暂时无法打开任务，请检查连接后重试")
		}
	}
	return err
}
