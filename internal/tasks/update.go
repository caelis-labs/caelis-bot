package tasks

import (
	"errors"
	"fmt"
)

// PauseForUpdate serializes the last native projection and durable receipt
// snapshot with task admission. Running workers remain owned by their runtime;
// only this host's new submissions and report dispatch are frozen.
func (m *Manager) PauseForUpdate(guard func() error) error {
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.refresh(); err != nil {
		return fmt.Errorf("无法保存原任务、请求回执和通知代际: %w", err)
	}
	if err := guard(); err != nil {
		return err
	}
	m.paused = true
	return nil
}

// PauseIfIdle shares admission with StartTask/SendTask/report delivery. Projection
// reads cannot authorize an update: refresh/persistence errors fail closed here.
// guard must not call back into Manager; it checks the resident conversation.
func (m *Manager) PauseIfIdle(guard func() error) error {
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.refresh(); err != nil {
		return err
	}
	m.mu.Lock()
	busy := false
	for _, r := range m.state.Records {
		if m.owns(r) && (!terminal(r.View.Status) || r.ReportState == "dispatching") {
			busy = true
			break
		}
	}
	m.mu.Unlock()
	if busy {
		return errors.New(m.text("host.pendingWorkOrReviewRemaining"))
	}
	if err := guard(); err != nil {
		return err
	}
	m.paused = true
	return nil
}

func (m *Manager) ResumeAfterUpdate() { m.op.Lock(); m.paused = false; m.op.Unlock() }
