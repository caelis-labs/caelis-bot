package machines

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) OwnsWork(runtime string) bool {
	p, ok := s.local.(api.WorkRouter)
	return ok && p.OwnsWork(runtime)
}

func (s *Service) BindWork(ctx context.Context, in api.TaskStart, id, runtime string) (string, error) {
	if in.Machine == "" {
		if p, ok := s.local.(api.WorkRouter); ok {
			return p.BindWork(ctx, in, id, runtime)
		}
		return runtime, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bindLocked(in, id, runtime)
}

func (s *Service) bindLocked(in api.TaskStart, id, runtime string) (string, error) {
	if old, exists := s.state.Routes[id]; exists {
		bound := s.state.Runtimes[id]
		if old != in.Machine || bound == "" || runtime != "" && runtime != bound {
			return "", errors.New("task_target_conflict")
		}
		return bound, nil
	}
	p, ok := s.state.Profiles[in.Machine]
	if !ok || p.View.State != "ready" {
		return "", errors.New("machine_not_ready")
	}
	if runtime == "" {
		runtime = p.View.Runtime
	}
	if runtime != "codex" && runtime != "caelis" {
		return "", errors.New("original_task_runtime_unavailable")
	}
	s.state.Routes[id], s.state.Runtimes[id] = in.Machine, runtime
	if err := s.save(); err != nil {
		delete(s.state.Routes, id)
		delete(s.state.Runtimes, id)
		return "", err
	}
	return runtime, nil
}
