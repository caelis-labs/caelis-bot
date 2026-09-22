package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type preferences interface {
	Load() (Placement, error)
	Save(Placement) error
}
type fileStore struct{ path string }

func (s fileStore) Load() (Placement, error) {
	p := defaults()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(b, &p)
	if err != nil {
		return defaults(), err
	}
	return p, nil
}
func (s fileStore) Save(p Placement) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".placement-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path)
}
