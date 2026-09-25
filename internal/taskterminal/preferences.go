package taskterminal

import (
	"errors"
	"path/filepath"
	"strings"
)

type Choice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

var ErrUnsupportedDefault = errors.New("system default terminal has no supported adapter")

func BundleID(preference string) string {
	switch preference {
	case "terminal":
		return "com.apple.Terminal"
	case "iterm2":
		return "com.googlecode.iterm2"
	case "ghostty":
		return "com.mitchellh.ghostty"
	}
	return ""
}
func PreferenceForBundle(bundle string) string {
	for _, p := range []string{"terminal", "iterm2", "ghostty"} {
		if strings.EqualFold(bundle, BundleID(p)) {
			return p
		}
	}
	return ""
}
func Choices(installed func(string) bool) []Choice {
	return []Choice{{ID: "system", Available: true}, {ID: "terminal", Name: "Terminal", Available: installed(BundleID("terminal"))}, {ID: "iterm2", Name: "iTerm2", Available: installed(BundleID("iterm2"))}, {ID: "ghostty", Name: "Ghostty", Available: installed(BundleID("ghostty"))}}
}

// OpenArgs never accepts arbitrary shell templates. The private, quoted attach
// script remains shared across terminal adapters.
func OpenArgs(preference, path string) ([]string, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return nil, errors.New("invalid launch script")
	}
	if preference == "system" {
		return []string{path}, nil
	}
	id := BundleID(preference)
	if id == "" {
		return nil, errors.New("unsupported terminal preference")
	}
	if preference == "ghostty" {
		// macOS forwards CLI flags only to a new instance. Avoid restoring the
		// user's saved windows into it; close this instance when its TUI exits.
		return []string{"-n", "-b", id, "--args", "--window-save-state=never", "--quit-after-last-window-closed=true", "-e", "/bin/sh", path}, nil
	}
	return []string{"-b", id, path}, nil
}
