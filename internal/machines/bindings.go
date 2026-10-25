package machines

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"time"
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
	if s.loadErr != nil {
		return "", s.loadErr
	}
	if old, exists := s.state.Routes[id]; exists {
		bound := s.state.Runtimes[id]
		if old != in.Machine || bound == "" || runtime != "" && runtime != bound {
			return "", errors.New("task_target_conflict")
		}
		// Restoring a durable visible task ledger retains its original owner,
		// including a crash between writing that ledger and attempting dispatch.
		if runtime != "" && s.state.Reservations[id] {
			reservationKey := s.state.ReservationKeys[id]
			delete(s.state.Reservations, id)
			delete(s.state.ReservationKeys, id)
			if err := s.save(); err != nil {
				s.state.Reservations[id] = true
				if s.state.ReservationKeys == nil {
					s.state.ReservationKeys = map[string]string{}
				}
				s.state.ReservationKeys[id] = reservationKey
				return "", err
			}
		}
		return bound, nil
	}
	p, ok := s.state.Profiles[in.Machine]
	if !ok || p.View.PairingStatus != "approved" || p.View.State != "ready" || p.View.Archived || p.View.ConnectionType == "bot" && !botProfileReady(p.View) {
		return "", errors.New("machine_not_ready")
	}
	reservation := runtime == ""
	if runtime == "" {
		runtime = p.View.Runtime
	}
	if runtime != "codex" && runtime != "caelis" {
		return "", errors.New("original_task_runtime_unavailable")
	}
	s.state.Routes[id], s.state.Runtimes[id] = in.Machine, runtime
	if reservation {
		s.state.Reservations[id] = true
		if s.state.ReservationKeys == nil {
			s.state.ReservationKeys = map[string]string{}
		}
		s.state.ReservationKeys[id] = rand.Text()
	}
	if err := s.save(); err != nil {
		delete(s.state.Routes, id)
		delete(s.state.Runtimes, id)
		delete(s.state.Reservations, id)
		delete(s.state.ReservationKeys, id)
		return "", err
	}
	return runtime, nil
}

func (s *Service) ReleaseWorkPreparation(id string) error {
	s.mu.Lock()
	if !s.state.Reservations[id] {
		s.mu.Unlock()
		return nil
	}
	machine, runtime, key := s.state.Routes[id], s.state.Runtimes[id], s.state.ReservationKeys[id]
	p := s.state.Profiles[machine]
	s.mu.Unlock()
	if p.View.ConnectionType == "bot" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if _, err := workerpeer.Call(ctx, p.Peer, workerpeer.Request{Action: "release_preparation", ID: id, Runtime: runtime}); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A late acknowledgement can release only this unchanged undispatched lease.
	current := s.state.Profiles[machine]
	if !s.state.Reservations[id] || s.state.Routes[id] != machine || s.state.Runtimes[id] != runtime || s.state.ReservationKeys[id] != key || !sameConnectionConfiguration(current, p) {
		return nil
	}
	delete(s.state.Routes, id)
	delete(s.state.Runtimes, id)
	delete(s.state.Reservations, id)
	delete(s.state.ReservationKeys, id)
	if err := s.save(); err != nil {
		s.state.Routes[id], s.state.Runtimes[id], s.state.Reservations[id] = machine, runtime, true
		if s.state.ReservationKeys == nil {
			s.state.ReservationKeys = map[string]string{}
		}
		s.state.ReservationKeys[id] = key
		return err
	}
	return nil
}
