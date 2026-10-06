package taskterminal

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// LaunchArguments is an argv boundary. The native terminal owner decides how
// to deliver this command to its existing owned window; no string is executed
// by a shell in this representation.
type LaunchArguments struct {
	Binary    string
	Directory string
	Args      []string
	Env       map[string]string
}

func TerminalArguments(t api.TerminalTarget) (LaunchArguments, error) {
	if !filepath.IsAbs(t.Binary) || !filepath.IsAbs(t.Directory) {
		return LaunchArguments{}, errors.New("invalid native terminal target")
	}
	for _, value := range []string{t.Binary, t.Directory, t.Endpoint, t.Thread, t.CodexHome, t.Session, t.Store, t.TokenFile} {
		if strings.ContainsRune(value, 0) {
			return LaunchArguments{}, errors.New("invalid terminal argument")
		}
	}
	result := LaunchArguments{Binary: t.Binary, Directory: t.Directory, Env: map[string]string{}}
	switch t.Runtime {
	case "codex":
		if !strings.HasPrefix(t.Endpoint, "unix:///") || t.Thread == "" || strings.HasPrefix(t.Thread, "-") {
			return LaunchArguments{}, errors.New("invalid Codex terminal target")
		}
		if t.CodexHome != "" {
			result.Env["CODEX_HOME"] = t.CodexHome
		}
		result.Args = []string{"-c", "check_for_update_on_startup=false", "--remote", t.Endpoint, "resume", t.Thread}
	case "caelis":
		u, err := url.Parse(t.Endpoint)
		if err != nil || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || t.Session == "" || strings.HasPrefix(t.Session, "-") || !filepath.IsAbs(t.Store) || !filepath.IsAbs(t.TokenFile) {
			return LaunchArguments{}, errors.New("invalid Caelis terminal target")
		}
		result.Args = []string{"attach", "--control-url", t.Endpoint, "--session", t.Session, "--store-dir", t.Store, "--control-token-file", t.TokenFile}
	case "setup":
		result.Args = []string{}
	default:
		return LaunchArguments{}, errors.New("unsupported terminal runtime")
	}
	return result, nil
}
