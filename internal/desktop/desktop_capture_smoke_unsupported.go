//go:build !darwin || !cgo

package desktop

import "errors"

func RunDesktopCaptureSmoke(string, string, string, string) error {
	return errors.New("desktop capture acceptance requires the macOS host")
}
