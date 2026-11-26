package desktop

import (
	"errors"
	"os"
)

func openLocalRegularFile(path string) (*os.File, error) {
	v, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !v.Mode().IsRegular() {
		return nil, errors.New("file unavailable")
	}
	return os.Open(path)
}
