package desktop

func (s *Service) WorkerTaskSelection() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workerTaskSelection
}
func (s *Service) CloseWorkerTask() { s.mu.Lock(); s.workerTaskSelection = ""; s.mu.Unlock() }

// CloseWorkerTaskWindow releases presentation only. Task ownership, decisions
// and the Runtime remain with the original task services.
func (s *Service) CloseWorkerTaskWindow(id string) {
	if s.closeWorkerWindow != nil {
		s.closeWorkerWindow(id)
	}
}

func (s *Service) notifyWorkerWindows(script string) {
	if s.workerWindowsChanged != nil {
		s.workerWindowsChanged(script)
	}
}
