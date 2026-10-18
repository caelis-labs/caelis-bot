//go:build windows

package taskterminal

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

var ErrNativeTerminalUnsupported = errors.New("Windows console terminal target is unsupported")

type windowsLaunch struct {
	Version        int             `json:"version"`
	Command        LaunchArguments `json:"command"`
	Pending        string          `json:"pending,omitempty"`
	AcceptedPrefix string          `json:"acceptedPrefix,omitempty"`
	Observer       bool            `json:"observer,omitempty"`
}

// A Windows launch file is a typed JSON descriptor, never executable shell text.
func Script(t api.TerminalTarget) (string, error) {
	// Remote routes require a separate native SSH transport. Never resume a
	// remote thread against the local App Server when that transport is absent.
	if len(t.SSH) != 0 {
		return "", ErrNativeTerminalUnsupported
	}
	args, err := TerminalArguments(t)
	if err != nil {
		return "", errors.Join(ErrNativeTerminalUnsupported, err)
	}
	b, err := json.Marshal(windowsLaunch{Version: 1, Command: args, Observer: t.Runtime == "caelis" || t.Runtime == "codex"})
	return string(b), err
}
func guardedScript(pending, prefix, script string) (string, error) {
	var v windowsLaunch
	if json.Unmarshal([]byte(script), &v) != nil || v.Version != 1 || !filepath.IsAbs(pending) || filepath.Dir(pending) != filepath.Dir(prefix) {
		return "", ErrNativeTerminalUnsupported
	}
	v.Pending, v.AcceptedPrefix = pending, prefix
	b, err := json.Marshal(v)
	return string(b), err
}
