//go:build !darwin || !cgo

package desktop

import "errors"

func RunRemoteTerminalSmoke(string, string, string) error {
	return errors.New("native terminal acceptance requires macOS")
}
