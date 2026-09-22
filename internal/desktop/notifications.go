package desktop

type notificationDriver interface {
	notify(id, title, body string, reminder bool)
	notificationStatus() string
	configureNotifications()
}

// Delivery is owned by the native host, independent of webview polling. A click
// opens chat; it never approves, retries, unhides the pet or executes a prompt.
func (s *Service) Notify(id, title, body string, reminder bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(notificationDriver); ok && !s.stopped {
		d.notify(id, title, body, reminder)
	}
}
func (s *Service) NotificationStatus() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(notificationDriver); ok && !s.stopped {
		return d.notificationStatus()
	}
	return "unavailable"
}
func (s *Service) ConfigureNotifications() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(notificationDriver); ok && !s.stopped {
		d.configureNotifications()
	}
}
