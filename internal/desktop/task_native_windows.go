package desktop

import (
	"context"
	"encoding/base64"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"slices"
	"time"
)

func taskSnapshotStatus() string    { return "available" }
func configureTaskSnapshots() error { return nil }

// Windows has a contextual task popover. It exposes only the same owned-card
// operations as AppKit; it cannot supply terminal argv or execution targets.
func (s *Service) TaskDockState() []api.TaskPreview {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.taskPreviews)
}
func (s *Service) WindowsTaskDockExpanded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.native.(*windowsDriver)
	return ok && !s.stopped && d.taskExpanded.Load()
}
func (s *Service) SetWindowsTaskDockExpanded(expanded bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(*windowsDriver); ok && !s.stopped {
		d.setTaskDockExpanded(expanded, false)
	}
}
func (s *Service) DismissTaskTerminal(id string) error {
	return s.dismissTask(context.Background(), id)
}
func (s *Service) LockTaskCard(id string, locked bool) error {
	if !s.taskWindowAllowed(id) {
		return taskterminal.ErrWindowIdentity
	}
	return s.manageTaskList(id, locked, false)
}
func (s *Service) ClearTaskCards() error { return s.manageTaskList("", false, true) }
func (s *Service) MoveTaskCard(id, before string) error {
	if !s.taskWindowAllowed(id) || before != "" && !s.taskWindowAllowed(before) {
		return taskterminal.ErrWindowIdentity
	}
	return s.moveTask(id, before)
}

func (s *Service) observeWindowsTaskTerminal(ctx context.Context, id string, manager *taskterminal.WindowManager) {
	s.mu.Lock()
	event := s.taskWindowEvents[id]
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	pixels, err := manager.Thumbnail(ctx, id)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	s.storeWindowsTaskThumbnail(id, event.Revision, pixels, err)
}

func (s *Service) storeWindowsTaskThumbnail(id string, revision uint64, pixels []byte, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || !s.started || revision == 0 || s.taskWindowEvents[id].Revision != revision || !slices.ContainsFunc(s.taskPreviews, func(t api.TaskPreview) bool { return t.ID == id }) {
		return
	}
	if driver, ok := s.native.(*windowsDriver); ok {
		if err != nil || len(pixels) == 0 {
			delete(driver.taskThumbnails, id)
		} else {
			if driver.taskThumbnails == nil {
				driver.taskThumbnails = map[string]windowsTaskThumbnail{}
			}
			driver.taskThumbnails[id] = windowsTaskThumbnail{Data: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(pixels), Captured: time.Now(), Generation: revision >> 32}
		}
		if driver.taskWindow != nil {
			driver.taskWindow.ExecJS("window.dispatchEvent(new Event('tasks-changed'))")
		}
	}
}

func (s *Service) WindowsTaskThumbnail(id string) string {
	if !s.taskWindowAllowed(id) {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if driver, ok := s.native.(*windowsDriver); ok && !s.stopped {
		thumbnail := driver.taskThumbnails[id]
		if time.Since(thumbnail.Captured) < 30*time.Second && thumbnail.Generation == s.taskWindowEvents[id].Revision>>32 {
			return thumbnail.Data
		}
	}
	return ""
}

type windowsTaskThumbnail struct {
	Data       string
	Captured   time.Time
	Generation uint64
}

// Called under Service.mu after the authoritative list changes.
func (d *windowsDriver) pruneTaskThumbnails(ids []string) {
	for id := range d.taskThumbnails {
		if !slices.Contains(ids, id) {
			delete(d.taskThumbnails, id)
		}
	}
}
