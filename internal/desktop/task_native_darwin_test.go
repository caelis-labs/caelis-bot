//go:build darwin && cgo

package desktop

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"testing"
)

type placedTaskController struct {
	taskControllerStub
	point taskterminal.Point
	id    string
}

func (w *placedTaskController) Place(_ context.Context, id string, p taskterminal.Point) error {
	w.id = id
	w.point = p
	return nil
}
func TestTaskDropDispatchesExplicitPlacementInsteadOfClick(t *testing.T) {
	s, _ := taskService(t)
	s.observeTasks([]api.TaskPreview{{ID: "owned"}})
	w := &placedTaskController{taskControllerStub: taskControllerStub{click: func(context.Context, string) error { t.Fatal("drop toggled window"); return nil }}}
	s.taskWindows = w
	p := taskterminal.Point{X: -400, Y: 500}
	if err := s.openTaskAt(t.Context(), "owned", &p); err != nil || w.point != p || w.id != "owned" {
		t.Fatal(err, w)
	}
	if err := s.openTaskAt(t.Context(), "foreign", &p); err == nil {
		t.Fatal("foreign placement accepted")
	}
}
