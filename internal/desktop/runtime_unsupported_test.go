//go:build !darwin || !cgo

package desktop

import (
	"runtime"
	"strings"
	"testing"
)

func TestUnsupportedHostCannotReportSuccessfulLaunch(t *testing.T) {
	err := Run(nil)
	if err == nil || !strings.Contains(err.Error(), runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("unsupported launch must report its platform: %v", err)
	}
}
