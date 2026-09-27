//go:build !darwin || !cgo

package desktop

import "errors"

func RunTerminalSmoke([]string) error {
	return errors.New("terminal acceptance requires the macOS host")
}
