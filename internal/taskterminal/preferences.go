package taskterminal

import (
	"errors"
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
