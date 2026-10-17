package desktop

import (
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"testing"
)

func TestWindowsUpdatePausesTerminalSubmissionWithoutReplay(t *testing.T) {
	g := &windowsTerminalGate{}
	submitted := 0
	submit := func() (taskterminal.Window, error) { submitted++; return nil, nil }
	resume := g.pause()
	if _, err := g.launch(t.Context(), submit); err == nil || submitted != 0 {
		t.Fatal("update admitted terminal launch")
	}
	resume()
	if _, err := g.launch(t.Context(), submit); err != nil || submitted != 1 {
		t.Fatal("failed update did not resume explicit input", err)
	}
}
