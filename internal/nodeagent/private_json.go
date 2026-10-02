package nodeagent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func WritePrivateJSON(path string, value any) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
		return errors.New("private native file redirected")
	}
	b, err := json.Marshal(value)
	if err != nil || len(b) > 256<<10 {
		return errors.New("native journal limit")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".native-stage-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
