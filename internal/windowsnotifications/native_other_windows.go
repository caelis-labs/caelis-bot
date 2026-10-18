//go:build windows && !amd64

package windowsnotifications

import "errors"

// Native distribution is x64 only. Keep the shared preparation path compilable
// without claiming the amd64 WinRT bindings work on other Windows architectures.
type Manager struct{}

func New(Config, func(error)) *Manager         { return &Manager{} }
func (*Manager) Close()                        {}
func (*Manager) Status() string                { return "unsupported" }
func (*Manager) Notify(string, string, string) {}
func (*Manager) Dismiss(string)                {}
func Unregister(string, string) error          { return errors.New("native notifications require Windows x64") }
