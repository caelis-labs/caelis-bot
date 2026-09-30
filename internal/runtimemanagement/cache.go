package runtimemanagement

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ImportArchive accepts nonsecret artifact bytes through a native-only seam.
// It verifies the closed catalog's exact digest and size before making a private
// cache entry available. It never extracts, executes, or selects a runtime.
func (m *Manager) ImportArchive(ctx context.Context, provider, version string, source io.Reader) error {
	release, ok := m.release(provider, version)
	if !ok || source == nil {
		return errors.New("archive import requires a reviewed release")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	root, err := m.openRoot(true)
	if err != nil {
		return err
	}
	defer root.Close()
	unlock, err := lockRoot(ctx, root)
	if err != nil {
		return err
	}
	defer unlock()
	if err := privateMkdir(root, "archives"); err != nil {
		return errors.New("archive cache directory unavailable")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return errors.New("archive cache staging identity unavailable")
	}
	temporary := filepath.Join("archives", ".import-"+hex.EncodeToString(random))
	defer root.Remove(temporary)
	if err := saveArchive(ctx, root, temporary, release, source); err != nil {
		return err
	}
	// Only fully verified bytes replace an existing entry. Installation verifies
	// it again, so a damaged or interrupted cache cannot weaken the trust gate.
	if err := root.Rename(temporary, archiveCacheName(release)); err != nil {
		return errors.New("verified archive cache publication failed")
	}
	directory, err := root.Open("archives")
	if err != nil {
		return errors.New("archive cache persistence unavailable")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return errors.New("archive cache persistence unavailable")
	}
	return nil
}

func archiveCacheName(release Release) string {
	return filepath.Join("archives", release.SHA256+".tar.gz")
}

func (m *Manager) archiveSource(ctx context.Context, root *os.Root, release Release) (io.ReadCloser, error) {
	name := archiveCacheName(release)
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return m.fetch(ctx, release)
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !privateOwner(info) || info.Size() > maxArchive || (release.Size > 0 && info.Size() != release.Size) {
		return nil, errors.New("private archive cache entry is invalid")
	}
	if err := privateParents(root, name); err != nil {
		return nil, errors.New("private archive cache directory is invalid")
	}
	source, err := root.Open(name)
	if err != nil {
		return nil, errors.New("private archive cache unavailable")
	}
	return source, nil
}
