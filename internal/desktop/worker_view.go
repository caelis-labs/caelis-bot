package desktop

func (s *Service) WorkerTaskSelection() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workerTaskSelection
}
func (s *Service) CloseWorkerTask() { s.mu.Lock(); s.workerTaskSelection = ""; s.mu.Unlock() }
