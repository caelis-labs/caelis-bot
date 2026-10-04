package desktopcontrol

import (
	"fmt"
	"time"
	"unicode/utf16"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
)

// Check the entire plan before any prefix can dispatch. The helper still owns
// native validation, focus borrowing, the 1-second input budget and cleanup.
// Never split, truncate or rewrite a rejected plan into separate transactions.
func cooperativePlan(raw []byte) error {
	var plan dw.Plan
	if err := protocol.Decode(raw, &plan); err != nil {
		return fmt.Errorf("invalid desktop plan: %w", err)
	}
	for _, s := range plan.Steps {
		if s.Target.Point != nil || s.Drag != nil && s.Drag.To.Point != nil {
			return fmt.Errorf("step %s: raw Point input is unavailable; use an observed Ref or target anchor", s.ID)
		}
		if s.TypeText != nil && len(utf16.Encode([]rune(s.TypeText.Text))) > 256 {
			return fmt.Errorf("step %s: cooperative type_text accepts at most 256 UTF-16 units; use semantic set_value for supported fields", s.ID)
		}
		if s.Drag != nil && s.Drag.Duration > 500*time.Millisecond {
			return fmt.Errorf("step %s: cooperative drag duration must be at most 500ms", s.ID)
		}
	}
	return nil
}
