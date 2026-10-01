//go:build !linux && !darwin

package nodeagent

import "errors"

func lockAgent(string) (func(), error) {
	return nil, errors.New("agent owner process lock unsupported")
}
