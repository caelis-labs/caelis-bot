//go:build windows

package taskterminal

import (
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWindowsTerminalRefusesPOSIXLaunch(t *testing.T) {
	_, err := Script(api.TerminalTarget{Runtime: "codex", Binary: `C:\Codex\codex.exe`, Directory: `C:\Work`, Endpoint: `npipe://./pipe/codex`, Thread: "original"})
	if !errors.Is(err, ErrNativeTerminalUnsupported) {
		t.Fatalf("unsupported native launch must be explicit: %v", err)
	}
}
