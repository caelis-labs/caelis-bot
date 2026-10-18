package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// modelSelections separates user intent from Runtime availability. Only settings
// are retried, never messages, approvals, tools or unknown native operations.
// The last choice wins independently for conversation and new background work.
type modelSelections struct {
	mu      sync.Mutex
	path    string
	choices map[string]api.ModelSelectionState
	wake    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	flight  context.CancelFunc
	done    chan struct{}
	started bool
	apply   func(context.Context, string, string, api.WorkExecutionSettings) error
	log     *diagnosticlog.Logger
}

func newModelSelections(root string, apply func(context.Context, string, string, api.WorkExecutionSettings) error, log *diagnosticlog.Logger) *modelSelections {
	ctx, cancel := context.WithCancel(context.Background())
	m := &modelSelections{path: filepath.Join(root, "model-selections.json"), choices: map[string]api.ModelSelectionState{}, wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel, done: make(chan struct{}), apply: apply, log: log}
	if data, err := os.ReadFile(m.path); err == nil {
		var stored map[string]api.ModelSelectionState
		if json.Unmarshal(data, &stored) == nil {
			for scope, choice := range stored {
				if validModelSelection(scope, choice.Runtime, choice.Selection) == nil && choice.Revision > 0 {
					choice.Scope = scope
					if choice.State != "applied" {
						choice.State = "pending"
					}
					m.choices[scope] = choice
				}
			}
		}
	}
	return m
}

func validModelSelection(scope, runtime string, value api.WorkExecutionSettings) error {
	if scope != "conversation" && scope != "work" {
		return errors.New("invalid model scope")
	}
	if runtime != "codex" && runtime != "caelis" && !(scope == "work" && runtime == "conversation" && value == (api.WorkExecutionSettings{})) {
		return errors.New("invalid model runtime")
	}
	if scope == "conversation" && value.Model == "" {
		return errors.New("select a model")
	}
	return api.ValidateExecutionSettings(api.ExecutionSettings{Model: value.Model, Effort: value.Effort, ServiceTier: value.ServiceTier})
}

func (m *modelSelections) Save(scope, runtime string, value api.WorkExecutionSettings) error {
	if err := validModelSelection(scope, runtime, value); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return m.ctx.Err()
	}
	previous := m.choices[scope]
	if previous.Runtime == runtime && previous.Selection == value && previous.State != "applied" {
		return nil
	}
	m.choices[scope] = api.ModelSelectionState{Scope: scope, Runtime: runtime, Selection: value, Revision: previous.Revision + 1, State: "pending"}
	if err := m.persist(); err != nil {
		// Keep the draft in memory and retry storage before touching a Runtime.
		choice := m.choices[scope]
		choice.State = "saving"
		m.choices[scope] = choice
		m.record("model_selection_storage_retry")
	}
	if m.flight != nil {
		m.flight()
	}
	m.startLocked()
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return nil
}

func (m *modelSelections) Snapshot() []api.ModelSelectionState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []api.ModelSelectionState{}
	for _, scope := range []string{"conversation", "work"} {
		if v, ok := m.choices[scope]; ok {
			out = append(out, v)
		}
	}
	return out
}

func (m *modelSelections) Start() { m.mu.Lock(); defer m.mu.Unlock(); m.startLocked() }
func (m *modelSelections) startLocked() {
	if m.started || m.ctx.Err() != nil {
		return
	}
	m.started = true
	go m.run()
}
func (m *modelSelections) Close() {
	m.mu.Lock()
	m.cancel()
	started := m.started
	m.mu.Unlock()
	if started {
		<-m.done
	}
}
func (m *modelSelections) persist() error { return localstate.Write(m.path, m.choices) }
func (m *modelSelections) record(code string) {
	if m.log != nil {
		m.log.Write(diagnosticlog.Record{Component: "model_selection", Code: code})
	}
}

func (m *modelSelections) run() {
	defer close(m.done)
	delay := time.Second
	for m.ctx.Err() == nil {
		pending := false
		for _, scope := range []string{"conversation", "work"} {
			m.mu.Lock()
			choice, ok := m.choices[scope]
			if !ok || choice.State == "applied" {
				m.mu.Unlock()
				continue
			}
			pending = true
			if m.persist() != nil {
				m.mu.Unlock()
				continue
			}
			ctx, cancel := context.WithTimeout(m.ctx, 25*time.Second)
			m.flight = cancel
			choice.State = "applying"
			m.choices[scope] = choice
			m.mu.Unlock()
			err := m.apply(ctx, scope, choice.Runtime, choice.Selection)
			cancel()
			m.mu.Lock()
			m.flight = nil
			if current := m.choices[scope]; current.Revision == choice.Revision {
				if err == nil {
					choice.State = "applied"
					delay = time.Second
				} else {
					choice.State = "recovering"
					m.record("model_selection_retry")
				}
				m.choices[scope] = choice
				if m.persist() != nil {
					// A successful native preference can be safely read/reapplied;
					// keep retrying until the local acknowledgement is durable too.
					choice.State = "saving"
					m.choices[scope] = choice
				}
			}
			m.mu.Unlock()
		}
		if !pending {
			select {
			case <-m.ctx.Done():
				return
			case <-m.wake:
			}
			delay = time.Second
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-m.ctx.Done():
			timer.Stop()
			return
		case <-m.wake:
			timer.Stop()
			delay = time.Second
		case <-timer.C:
			delay = min(delay*2, 30*time.Second)
		}
	}
}

func (s *runtimeSetup) ModelSelections() []api.ModelSelectionState {
	return s.app.modelSelections.Snapshot()
}
