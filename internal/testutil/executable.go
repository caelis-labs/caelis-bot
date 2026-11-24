package testutil

import (
	"errors"
	"io"
	"os"
	"runtime"
)

// StageExecutable places read-only fixture bytes at a path the test must
// exercise. Use the original executable directly when its path does not matter.
func StageExecutable(source, target string) error {
	// On Windows, RemoveAll's image-section check can reject a hard link
	// to a running executable even after the staged child has exited.
	if runtime.GOOS != "windows" {
		if err := os.Link(source, target); err == nil {
			return nil
		}
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}
