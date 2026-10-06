//go:build windows

package taskterminal

import (
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

var ErrNativeTerminalUnsupported = errors.New("Windows task terminal launch is not implemented")

func Script(api.TerminalTarget) (string, error) { return "", ErrNativeTerminalUnsupported }
func guardedScript(string, string, string) (string, error) {
	return "", ErrNativeTerminalUnsupported
}
