//go:build !darwin && !linux

package nodecoord

import "errors"

func lockDirectory(string) (func(), error) {
	return nil, errors.New("broker private ownership lock unsupported on this platform")
}
