//go:build !windows

package taskterminal

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"strings"
)

func caelisTerminalArguments(target api.TerminalTarget) []string {
	return []string{"attach", "--control-url", target.Endpoint, "--session", target.Session, "--store-dir", target.Store, "--control-token-file", target.TokenFile}
}

func validLocalEndpoint(endpoint string) bool { return strings.HasPrefix(endpoint, "unix:///") }
func NativeTerminalPreference(string) bool    { return false }
func ValidateCustomCommand(command string) error {
	_, err := CustomArgs(command, "/private/terminal.command")
	return err
}
