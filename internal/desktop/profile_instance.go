package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// Explicit profiles own independent native instances. The default profile
// retains its original key, including an explicit path to that same profile.
// Product identity and renderer navigation remain independent of this OS lock.
func profileInstanceID(appID, profile, defaultProfile string) (string, error) {
	canonical, err := canonicalProfilePath(profile)
	if err != nil {
		return "", err
	}
	canonicalDefault, err := canonicalProfilePath(defaultProfile)
	if err != nil {
		return "", err
	}
	if canonical == canonicalDefault {
		return appID, nil
	}
	sum := sha256.Sum256([]byte(canonical))
	return appID + ".profile." + hex.EncodeToString(sum[:]), nil
}

// Resolve existing ancestors too: a not-yet-created profile beneath a symlink
// must acquire the same lock as its eventual canonical directory.
func canonicalProfilePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("native profile path must be absolute")
	}
	path = filepath.Clean(path)
	for ancestor := path; ; ancestor = filepath.Dir(ancestor) {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			resolved, err = canonicalExistingProfilePath(resolved)
			if err != nil {
				return "", errors.New("native profile path could not be resolved")
			}
			suffix, err := filepath.Rel(ancestor, path)
			if err != nil {
				return "", errors.New("native profile path could not be resolved")
			}
			return filepath.Join(resolved, suffix), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("native profile path could not be resolved")
		}
		if _, statErr := os.Lstat(ancestor); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
			return "", errors.New("native profile path could not be resolved")
		}
		if ancestor == filepath.Dir(ancestor) {
			return "", errors.New("native profile path could not be resolved")
		}
	}
}
