//go:build !darwin || !cgo

package desktop

import (
	"fmt"
	"io/fs"
	"runtime"
)

// Run deliberately fails before creating surfaces or changing preferences. A
// buildable shared core is not a working native desktop port.
func Run(_ fs.FS) error {
	return fmt.Errorf("Caelis Bot desktop is not implemented for this build (%s/%s); the current native host requires macOS with CGO_ENABLED=1; see docs/platform-baseline.md", runtime.GOOS, runtime.GOARCH)
}
