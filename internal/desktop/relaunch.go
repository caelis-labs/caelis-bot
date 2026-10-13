package desktop

import (
	"os/exec"
	"strconv"
	"strings"
)

func runtimeRelaunchCommand(executable, root string, pid int, research bool, home, codexHome, path string) *exec.Cmd {
	// Preserve the original owner's exact roots. In a Research build a default
	// Launch Services relaunch would recover the real account's HOME and PATH.
	script := `while kill -0 "$1" 2>/dev/null; do sleep 0.1; done; exec "$2" --env "CAELIS_BOT_DATA_DIR=$4" "$3"`
	target, launcher := executable, executable
	if i := strings.LastIndex(executable, ".app/Contents/MacOS/"); i >= 0 {
		target, launcher = executable[:i+4], "/usr/bin/open"
		if research {
			script = `while kill -0 "$1" 2>/dev/null; do sleep 0.1; done; exec "$2" --env "CAELIS_BOT_DATA_DIR=$4" --env "HOME=$5" --env "ZDOTDIR=$5" --env "CODEX_HOME=$6" --env "PATH=$7" "$3"`
		}
	} else {
		script = `while kill -0 "$1" 2>/dev/null; do sleep 0.1; done; exec "$2"`
	}
	args := []string{"-c", script, "caelis-relaunch", strconv.Itoa(pid), launcher, target, root}
	if research && launcher == "/usr/bin/open" {
		args = append(args, home, codexHome, path)
	}
	return exec.Command("/bin/sh", args...)
}
