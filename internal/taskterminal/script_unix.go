//go:build !windows

package taskterminal

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func Script(t api.TerminalTarget) (string, error) {
	if (t.Binary != "" || t.Runtime != "setup") && !filepath.IsAbs(t.Binary) || !filepath.IsAbs(t.Directory) {
		return "", errors.New("invalid native terminal target")
	}
	for _, v := range []string{t.Binary, t.Directory, t.Endpoint, t.Thread, t.CodexHome, t.Session, t.Store, t.TokenFile} {
		if strings.ContainsRune(v, 0) {
			return "", errors.New("invalid terminal argument")
		}
	}
	script := "#!/bin/sh\nunset NO_COLOR\n"
	switch t.Runtime {
	case "setup":
		if t.Binary == "" {
			script += "exec /bin/sh -l\n"
		} else {
			script += "cd " + quote(t.Directory) + " || exit 1\nexec " + quote(t.Binary) + "\n"
		}
	case "codex":
		spec, err := TerminalArguments(t)
		if err != nil {
			return "", err
		}
		if home := spec.Env["CODEX_HOME"]; home != "" {
			script += "export CODEX_HOME=" + quote(home) + "\n"
		}
		script += renderArguments(spec)
	case "caelis":
		spec, err := TerminalArguments(t)
		if err != nil {
			return "", err
		}
		script += "unset CAELIS_CONTROL_URL CAELIS_CONTROL_TOKEN CAELIS_CONTROL_TOKEN_FILE CAELIS_CONTROL_EMBEDDED\n"
		script += renderArguments(spec)
	default:
		return "", errors.New("unsupported terminal runtime")
	}
	if len(t.SSH) > 0 {
		for _, arg := range t.SSH {
			if strings.ContainsRune(arg, 0) || strings.ContainsAny(arg, "\r\n") {
				return "", errors.New("invalid SSH terminal target")
			}
		}
		command := "exec /usr/bin/ssh"
		for _, arg := range t.SSH {
			command += " " + quote(arg)
		}
		script = "#!/bin/sh\nunset NO_COLOR\n" + command + " " + quote(strings.TrimPrefix(script, "#!/bin/sh\n")) + "\n"
	}
	return script, nil
}
func renderArguments(spec LaunchArguments) string {
	command := "cd " + quote(spec.Directory) + " || exit 1\nexec " + quote(spec.Binary)
	for _, arg := range spec.Args {
		command += " " + quote(arg)
	}
	return command + "\n"
}
func guardedScript(pending, acceptedPrefix, script string) (string, error) {
	rejectedPrefix := filepath.Join(filepath.Dir(pending), "rejected-")
	guard := "#!/bin/sh\nif ! /bin/mv " + quote(pending) + " " + quote(acceptedPrefix) + "\"$$\" 2>/dev/null; then\n  : > " + quote(rejectedPrefix) + "\"$$\"\n  printf '%s\\n' 'This terminal request has expired. Open the task again from Caelis Bot.'\n  exit 1\nfi\n"
	return guard + strings.TrimPrefix(script, "#!/bin/sh\n"), nil
}
