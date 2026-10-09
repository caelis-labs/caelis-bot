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
	// Terminal app choice is desktop presentation metadata, not backend state.
	type presentation struct {
		api.TaskPreview
		Terminal string `json:"terminal,omitempty"`
	}
	views := make([]presentation, len(tasks))
	terminal := ""
	if s.taskPreferences != nil {
		terminal = s.taskPreferences().Terminal
	}
	for i, task := range tasks {
		views[i] = presentation{TaskPreview: task, Terminal: terminal}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	data, _ := json.Marshal(views)
	current := make(map[string]api.TaskPreview, len(tasks))
	for _, task := range tasks {
		current[task.ID] = task
	}
	generationChanged := false
	for _, previous := range s.taskPreviews {
		if next, exists := current[previous.ID]; exists && previous.NoticeGeneration != next.NoticeGeneration {
			generationChanged = true
			break
		}
	}
	presentationChanged := string(data) != s.taskPreviewJSON
	if !presentationChanged && !generationChanged {
		return
	}
	if d, ok := s.native.(notificationDriver); ok {
		if len(s.taskPreviews) == 0 {
			// A prior process can leave A's notice behind after B was saved.
			for _, task := range tasks {
				if task.Status == "unknown" && task.NoticeGeneration != "" && !task.NoticeClaimed {
					d.dismissNotification("task-unknown-" + task.ID)
				}
			}
		}
		for _, previous := range s.taskPreviews {
			next := current[previous.ID]
			if previous.Status == "unknown" && (next.Status != "unknown" || previous.NoticeGeneration != "" && next.NoticeGeneration != "" && previous.NoticeGeneration != next.NoticeGeneration) {
				d.dismissNotification("task-unknown-" + previous.ID)
			}
		}
	}
	s.taskPreviewJSON = string(data)
	s.taskPreviews = slices.Clone(tasks)
	if d, ok := s.native.(taskDriver); ok && presentationChanged {
		d.tasks(string(data))
	}
}

// Explicit clicks are the only terminal-launch path. Presentation never chooses
// a toggle direction; the per-window controller reconciles the native state.
func (s *Service) openTask(ctx context.Context, id string) error {
	if !s.taskWindowAllowed(id) || s.taskWindows == nil {
		return errors.New("task is unavailable")
	}
	return s.taskWindows.Click(ctx, id)
}

func (s *Service) cancelTerminalOpening() {
	if s.taskWindows != nil {
		s.taskWindows.CancelAll()
	}
}

// UI close is stronger than Bot unpin: close only the owned attach client,
// then persist list removal. There is deliberately no StopWork dependency.
func (s *Service) dismissTask(ctx context.Context, id string) error {
	if !s.taskWindowAllowed(id) || s.taskWindows == nil {
		return errors.New("task is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := s.taskWindows.Dismiss(ctx, id); err != nil {
		if errors.Is(err, taskterminal.ErrWindowCloseCancelled) || errors.Is(err, taskterminal.ErrWindowClosePending) || errors.Is(err, context.Canceled) {
			return nil // Keep the card; user consent is neither failure nor exit.
		}
		s.taskListFailure("native.taskCloseFailed")
		return err
	}
	return s.unpinTask(id)
}
func (s *Service) moveTask(id, before string) error {
	if s.moveTaskPin == nil {
		return errors.New("task order is unavailable")
	}
	if err := s.moveTaskPin(id, before); err != nil {
		s.taskListFailure("native.taskListChangeFailed")
		return err
	}
	return nil
}
func (s *Service) taskListFailure(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(taskDriver); ok && !s.stopped {
		d.taskFailure(s.text(key, nil))
	}
}
