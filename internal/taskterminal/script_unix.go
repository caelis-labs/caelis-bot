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
	return terminalScript(t, "")
}

func terminalScript(t api.TerminalTarget, accept string) (string, error) {
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
			script += accept + "exec /bin/sh -l\n"
		} else {
			script += "cd " + quote(t.Directory) + " || exit 1\n" + accept + "exec " + quote(t.Binary) + "\n"
		}
	case "codex":
		spec, err := TerminalArguments(t)
		if err != nil {
			return "", err
		}
		if home := spec.Env["CODEX_HOME"]; home != "" {
			script += "export CODEX_HOME=" + quote(home) + "\n"
		}
		script += renderArguments(spec, accept)
	case "caelis":
		spec, err := TerminalArguments(t)
		if err != nil {
			return "", err
		}
		script += "unset CAELIS_CONTROL_URL CAELIS_CONTROL_TOKEN CAELIS_CONTROL_TOKEN_FILE CAELIS_CONTROL_EMBEDDED\n"
		script += renderArguments(spec, accept)
	default:
		return "", errors.New("unsupported terminal runtime")
	}

	return script, nil
}
func renderArguments(spec LaunchArguments, accept string) string {
	command := "if ! cd " + quote(spec.Directory) + " 2>/dev/null; then\n  printf '%s\\n' " + quote(api.ErrTaskWorkspaceUnavailable.Error()) + "\n  exit 1\nfi\n" + accept + "exec " + quote(spec.Binary)
	for _, arg := range spec.Args {
		command += " " + quote(arg)
	}
	return command + "\n"
}
func guardedScript(pending, acceptedPrefix string, target api.TerminalTarget) (string, error) {
	// cd/export/unset precede the one-use rename; no client can execute until
	// it wins that rename. This receipt proves cwd acquisition and delivery to
	// the exec boundary, never successful Runtime attachment.
	guard := "if ! /bin/mv " + quote(pending) + " " + quote(acceptedPrefix) + "\"$$\" 2>/dev/null; then\n  printf '%s\\n' 'This terminal request has expired. Open the task again from Caelis Bot.'\n  exit 1\nfi\n"
	return terminalScript(target, guard)
}
