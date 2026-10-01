//go:build !darwin && !linux

package app

import "errors"

func notebookProfileStopped(string) error {
	return errors.New("local Notebook owner confirmation unsupported on this platform")
}
