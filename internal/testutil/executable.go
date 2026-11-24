package testutil

import (
	"errors"
	"io"
	"os"
)

// LinkExecutable stages a read-only test executable under a different name.
// Copy only when the test's temporary directory is on a different volume or
// the filesystem does not support hard links. Callers may unlink the alias.
func LinkExecutable(source, target string) error {
	if err := os.Link(source, target); err == nil {
		return nil
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
