package workerwire

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// BindPair publishes immutable nonsecret origin/target metadata in the native
// owner's private directory before any foreign-source mutation is admitted.
// It never overwrites an existing pair or imports source-less Worker bindings.
func BindPair(directory string, pair Pair, retainedTasks bool) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return errors.New("Worker pairing requires a supported native node")
	}
	if !pair.valid() || !filepath.IsAbs(directory) {
		return errors.New("invalid native Worker pairing")
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil || canonical != directory {
		return errors.New("Worker pairing directory redirected")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !pairFileOwner(info) {
		return errors.New("Worker pairing directory must be private")
	}
	path := filepath.Join(directory, "worker-pair.json")
	check := func() error {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 8192 || !pairFileOwner(info) {
			return errors.New("Worker pairing file unavailable")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var existing Pair
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if d.Decode(&existing) != nil || d.Decode(new(any)) != io.EOF || existing != pair {
			return errors.New("Worker origin/target pairing changed")
		}
		return nil
	}
	syncDirectory := func() error {
		dir, err := os.Open(directory)
		if err != nil {
			return err
		}
		return errors.Join(dir.Sync(), dir.Close())
	}
	if err = check(); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return syncDirectory()
	}
	if retainedTasks {
		return errors.New("retained Worker tasks have no original origin pairing")
	}
	temp, err := os.CreateTemp(directory, ".worker-pair-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	data, _ := json.Marshal(pair)
	_, err = temp.Write(append(data, '\n'))
	if err == nil {
		err = temp.Sync()
	}
	err = errors.Join(err, temp.Close())
	if err != nil {
		return err
	}
	// Hard-link publication is exclusive; concurrent assembly cannot replace the
	// other owner's pairing. The temporary file is already complete and synced.
	if err = os.Link(name, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if err = check(); err != nil {
			return err
		}
	}
	if err = syncDirectory(); err != nil {
		return err
	}
	return check()
}
