package desktop

import (
	"errors"
	"os"
)

func openDevelopmentProfile(path string) (*os.File, error) {
	v, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !v.Mode().IsRegular() {
		return nil, errors.New("development profile invalid")
	}
	return os.Open(path)
}
