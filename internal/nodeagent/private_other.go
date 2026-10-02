//go:build !darwin && !linux

package nodeagent

import (
	"errors"
	"os"
)

func privateFileOwnedByCurrentUser(os.FileInfo) bool { return false }

func CheckPrivateDirectory(string) error {
	return errors.New("private node agent IPC unsupported on this platform")
}
func CaptureJoinSocket(string) (func(), error) {
	return nil, errors.New("private forwarded socket unsupported")
}
