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

func TestAssemblyTaskPlacementUsesLocalTerminalAndRemoteViewer(t *testing.T) {
	a, viewed := workerRoutingAssembly(t)
	s := a.Service
	w := &placedTaskController{taskControllerStub: taskControllerStub{click: func(context.Context, string) error {
		t.Fatal("placement toggled the terminal")
		return nil
	}}}
	s.taskWindows = w
	p := taskterminal.Point{X: -400, Y: 500}
	if err := s.openTaskAt(t.Context(), "local-codex", &p); err != nil || w.point != p || w.id != "local-codex" || *viewed != 0 {
		t.Fatalf("local placement failed: %v, window=%+v, viewed=%d", err, w, *viewed)
	}
	if err := s.openTaskAt(t.Context(), "peer", &p); err != nil || w.id != "local-codex" || *viewed != 1 || s.WorkerTaskSelection() != "peer" {
		t.Fatalf("remote placement reached native terminal: %v, window=%+v, viewed=%d", err, w, *viewed)
	}
	for _, id := range []string{"foreign", "stale"} {
		if err := s.openTaskAt(t.Context(), id, &p); err == nil {
			t.Fatalf("%s placement bypassed ownership", id)
		}
	}
	clicks := 0
	s.taskWindows = &taskControllerStub{click: func(_ context.Context, id string) error { clicks++; return nil }}
	if err := s.openTaskAt(t.Context(), "local-caelis", nil); err != nil || clicks != 1 {
		t.Fatal("nil point did not use normal local click", err, clicks)
	}
}
