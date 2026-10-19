package desktop

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const windowsLoginRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

type windowsLoginAtLogin struct {
	path, name, command string
	disableOnly         bool
}

func newWindowsLoginAtLogin() loginAtLoginController {
	_, id := applicationIdentity()
	exe, e := os.Executable()
	if e != nil || !filepath.IsAbs(exe) {
		return nil
	}
	return &windowsLoginAtLogin{path: windowsLoginRunKey, name: id, command: windows.ComposeCommandLine([]string{exe, "--login-launch"}), disableOnly: !windowsShellIntegration(exe, id)}
}
func (m *windowsLoginAtLogin) Status() LoginAtLoginStatus {
	if m.disableOnly {
		k, err := registry.OpenKey(registry.CURRENT_USER, m.path, registry.QUERY_VALUE)
		if err != nil {
			return LoginAtLoginStatus{State: "unsupported"}
		}
		defer k.Close()
		value, _, err := k.GetStringValue(m.name)
		if err != nil || value != m.command {
			return LoginAtLoginStatus{State: "unsupported"}
		}
		return LoginAtLoginStatus{Supported: true, State: "registered", Registered: true, Enabled: true}
	}
	k, e := registry.OpenKey(registry.CURRENT_USER, m.path, registry.QUERY_VALUE)
	if errors.Is(e, registry.ErrNotExist) {
		return LoginAtLoginStatus{Supported: true, State: "off"}
	}
	if e != nil {
		return LoginAtLoginStatus{Supported: true, State: "unavailable"}
	}
	defer k.Close()
	value, _, e := k.GetStringValue(m.name)
	if errors.Is(e, registry.ErrNotExist) {
		return LoginAtLoginStatus{Supported: true, State: "off"}
	}
	if e != nil || value != m.command {
		return LoginAtLoginStatus{Supported: true, State: "unavailable"}
	}
	// Run is the documented registration contract. Do not parse or rewrite the
	// private StartupApproved format; Windows policy owns effective execution.
	return LoginAtLoginStatus{Supported: true, State: "registered", Registered: true, Enabled: true}
}
func (m *windowsLoginAtLogin) Set(enabled bool) error {
	if enabled && m.disableOnly {
		return errLoginAtLoginUnsupported
	}
	if enabled && len([]uint16(windows.StringToUTF16(m.command))) > 260 {
		return errors.New("startup command exceeds the Windows Run limit; move the ZIP to a shorter path")
	}
	var k registry.Key
	var e error
	if enabled {
		k, _, e = registry.CreateKey(registry.CURRENT_USER, m.path, registry.QUERY_VALUE|registry.SET_VALUE)
	} else {
		// Disabling a historical ZIP entry must never create a registry key.
		k, e = registry.OpenKey(registry.CURRENT_USER, m.path, registry.QUERY_VALUE|registry.SET_VALUE)
		if errors.Is(e, registry.ErrNotExist) {
			return nil
		}
	}
	if e != nil {
		return e
	}
	defer k.Close()
	existing, _, e := k.GetStringValue(m.name)
	if e != nil && !errors.Is(e, registry.ErrNotExist) {
		return e
	}
	if e == nil && existing != m.command {
		return errors.New("a different installation owns this startup entry; turn it off there first")
	}
	if enabled {
		return k.SetStringValue(m.name, m.command)
	}
	if errors.Is(e, registry.ErrNotExist) {
		return nil
	}
	return k.DeleteValue(m.name)
}
func (*windowsLoginAtLogin) OpenSystemSettings() error {
	return windowsOpenURL("ms-settings:startupapps")
}
