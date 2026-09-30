//go:build !darwin && !linux

package nodeagent

import "errors"

func CheckPrivateDirectory(string) error {
	return errors.New("private node agent IPC unsupported on this platform")
}
