//go:build windows

package headless

import "errors"

func lock(string) (func(), error) {
	return nil, errors.New("use the Linux build in WSL for headless mode")
}
