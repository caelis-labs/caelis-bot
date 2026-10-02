package backend

func HasNativeNodeManagement(s *Service) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nodeManagement != nil
}

func (s *Service) productDraftPort() (ProductDraftPort, bool) {
	p, ok := s.engine.(ProductDraftPort)
	return p, ok
}
