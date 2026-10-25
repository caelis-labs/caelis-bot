package sshtransport

import (
	"strings"
	"sync"
)

// Bounded transient stderr is classified once; never returned, logged or saved.
type diagnostic struct {
	mu   sync.Mutex
	tail string
}

func (d *diagnostic) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tail += string(p)
	if len(d.tail) > 8192 {
		d.tail = d.tail[len(d.tail)-8192:]
	}
	return len(p), nil
}
func (d *diagnostic) code() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := strings.ToLower(d.tail)
	defer func() { d.tail = "" }()
	switch {
	case strings.Contains(s, "remote host identification has changed"):
		return "ssh_host_key_changed"
	case strings.Contains(s, "permission denied"):
		return "ssh_authentication_failed"
	case strings.Contains(s, "could not resolve hostname"):
		return "ssh_host_not_found"
	case strings.Contains(s, "connection refused") || strings.Contains(s, "connection timed out") || strings.Contains(s, "no route to host"):
		return "ssh_host_unreachable"
	case strings.Contains(s, "bad configuration") || strings.Contains(s, "bad configuration option"):
		return "ssh_config_syntax"
	}
	return "ssh_authentication_or_connection_failed"
}
