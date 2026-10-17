package taskterminal

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"path/filepath"
	"strings"
)

func caelisTerminalArguments(target api.TerminalTarget) []string {
	// WorkTerminal already authenticates the Store and native Session. A fixed
	// --control-url pins the old Host's ephemeral port and cannot survive its
	// replacement. Managed local discovery retains the same Session and lets
	// the released Client validate and reconnect to a newer Host in this Store.
	return []string{"--interactive", "--session", target.Session, "--store-dir", target.Store}
}

func validLocalEndpoint(endpoint string) bool {
	if !strings.HasPrefix(endpoint, "unix://") {
		return false
	}
	path := strings.TrimPrefix(endpoint, "unix://")
	volume := filepath.VolumeName(path)
	return filepath.IsAbs(path) && len(volume) == 2 && volume[1] == ':'
}
func NativeTerminalPreference(id string) bool {
	return id == "windows-console" || id == "windows-terminal"
}
func ValidateCustomCommand(command string) error {
	_, err := WindowsTemplateArgs(command, `C:\Bot\caelis-terminal.exe`, `C:\Bot\launch.command`)
	return err
}
