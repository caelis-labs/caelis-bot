package tasks

import "errors"

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
		if r.Provider == m.provider && (!terminal(r.View.Status) || r.ReportState == "dispatching") {
			busy = true
			break
		}
	}
	m.mu.Unlock()
	if busy {
		return errors.New("仍有未结束或结果待核对的独立工作")
	}
	if err := guard(); err != nil {
		return err
	}
	m.paused = true
	return nil
}

func (m *Manager) ResumeAfterUpdate() { m.op.Lock(); m.paused = false; m.op.Unlock() }
