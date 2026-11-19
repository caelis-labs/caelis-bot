package plugins

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Keep the shipped store location and package data layout across binary switches.
func OpenWorkspace(appRoot string) (*Manager, error) {
	if !filepath.IsAbs(appRoot) {
		return nil, errors.New("plugin application path must be absolute")
	}
	stable := filepath.Join(appRoot, "Plugins")
	// Recover a development build's split store if it was already opened locally.
	draft := filepath.Join(appRoot, "PluginState")
	present, err := directoryPresent(draft)
	if err != nil {
		return nil, err
	}
	if present {
		exists, err := directoryPresent(stable)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.New("conflicting plugin stores; both retained")
		}
		data := filepath.Join(appRoot, "Notebook", "plugins", "data")
		if has, err := directoryPresent(data); err != nil {
			return nil, err
		} else if has {
			target := filepath.Join(draft, "data")
			if exists, err := directoryPresent(target); err != nil {
				return nil, err
			} else if exists {
				return nil, errors.New("conflicting plugin data; both retained")
			}
			if err := ensureStoreDirectory(appRoot, filepath.Dir(data)); err != nil {
				return nil, err
			}
			if err := renameStoreDirectory(data, target); err != nil {
				return nil, err
			}
		}
		if err := renameStoreDirectory(draft, stable); err != nil {
			return nil, err
		}
	}
	return Open(stable)
}

func directoryPresent(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("plugin storage must be a real directory")
	}
	return true, nil
}

func renameStoreDirectory(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	if filepath.Dir(from) == filepath.Dir(to) {
		return localstate.SyncParent(to)
	}
	return errors.Join(localstate.SyncParent(to), localstate.SyncParent(from))
}
