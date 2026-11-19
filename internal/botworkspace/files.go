package botworkspace

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const fileLimit = 1 << 20

var ErrProjectWriteBusy = errors.New("workspace configuration is being edited")

func ensureDir(root *os.Root, path string) error {
	if err := root.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := root.Lstat(path)
	if err != nil || !info.IsDir() {
		return errors.New("workspace configuration directory is not a real directory")
	}
	return nil
}

func readRegular(root *os.Root, path string) ([]byte, error) {
	info, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > fileLimit {
		return nil, errors.New("invalid workspace file")
	}
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if actual, err := file.Stat(); err != nil || !os.SameFile(info, actual) {
		return nil, errors.New("workspace file changed while opening")
	}
	body, err := io.ReadAll(io.LimitReader(file, fileLimit+1))
	if len(body) > fileLimit {
		return nil, errors.New("workspace file is too large")
	}
	return body, err
}

// replace stages the full file before checking for edits since the original read.
func replace(root *os.Root, path string, prior, body []byte) error {
	if len(body) > fileLimit {
		return errors.New("workspace file is too large")
	}
	temp := filepath.Join(filepath.Dir(path), ".bot-write-"+rand.Text())
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	if err = localstate.ProtectOpenFile(f); err == nil {
		_, err = f.Write(body)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	current, err := readRegular(root, path)
	if err != nil {
		return err
	}
	if (current == nil) != (prior == nil) || !bytes.Equal(current, prior) {
		return ErrProjectWriteBusy
	}
	if err := root.Rename(temp, path); err != nil {
		return err
	}
	return localstate.SyncParent(filepath.Join(root.Name(), path))
}

// Keep the inode stable across updates so native and script writers serialize
// the complete read/merge/publish sequence. Closing the file releases the lock.
func lockConfig(root *os.Root) (*os.File, error) {
	const path = ".bot-extension.lock"
	f, err := root.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	info, statErr := root.Lstat(path)
	if err != nil || statErr != nil || !info.Mode().IsRegular() || !os.SameFile(actual, info) {
		f.Close()
		return nil, errors.New("invalid workspace configuration lock")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		busy, err := tryConfigLock(f)
		if !busy && err == nil {
			return f, nil
		}
		if !busy || time.Now().After(deadline) {
			f.Close()
			if busy {
				return nil, ErrProjectWriteBusy
			}
			return nil, err
		}
		time.Sleep(25 * time.Millisecond)
	}
}
