package secretstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

var namespaceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// FileStore places independent consumers under <data>/Credentials/<namespace>/.
// Legacy is read only when a local item is absent, then removed after a durable
// local write. No credential value, legacy key, or caller-provided path is used
// as a filename or returned to product UI.
type FileStore struct {
	Root, Namespace string
	Legacy          Store
	mu              sync.Mutex
}

type fileItem struct {
	Version int    `json:"version"`
	Secret  string `json:"secret"`
}

func (s *FileStore) path(id string) (string, error) {
	if !filepath.IsAbs(s.Root) || !namespaceName.MatchString(s.Namespace) || id == "" {
		return "", ErrUnavailable
	}
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(s.Root, s.Namespace, hex.EncodeToString(sum[:])+".json"), nil
}

func (s *FileStore) prepare(path string) error {
	for _, dir := range []string{s.Root, filepath.Dir(path)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe_secret_directory")
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileStore) checkDirectories(path string) error {
	for _, dir := range []string{s.Root, filepath.Dir(path)} {
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
			return errors.New("unsafe_secret_directory")
		}
	}
	return nil
}

func (s *FileStore) saveFile(path, secret string) error {
	if secret == "" || len(secret) > 256<<10 {
		return errors.New("invalid_secret_value")
	}
	if err := s.prepare(path); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("unsafe_secret_file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return localstate.WriteConfirmed(path, fileItem{Version: 1, Secret: secret})
}

func (s *FileStore) Save(id, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path(id)
	if err != nil {
		return err
	}
	_, oldErr := os.Lstat(path)
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return oldErr
	}
	if err := s.saveFile(path, secret); err != nil {
		return err
	}
	if errors.Is(oldErr, os.ErrNotExist) && s.Legacy != nil {
		if err := s.Legacy.Delete(id); err != nil {
			_ = os.Remove(path)
			return err
		}
	}
	return nil
}

func (s *FileStore) read(path string) (string, error) {
	if err := s.checkDirectories(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 260<<10 {
		return "", errors.New("unsafe_secret_file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var item fileItem
	dec := json.NewDecoder(io.LimitReader(f, 260<<10))
	if dec.Decode(&item) != nil || dec.Decode(new(any)) != io.EOF || item.Version != 1 || item.Secret == "" || len(item.Secret) > 256<<10 {
		return "", errors.New("invalid_secret_file")
	}
	return item.Secret, nil
}

func (s *FileStore) Load(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path(id)
	if err != nil {
		return "", err
	}
	value, err := s.read(path)
	if err == nil || !errors.Is(err, os.ErrNotExist) || s.Legacy == nil {
		return value, err
	}
	value, err = s.Legacy.Load(id)
	if err != nil {
		return "", err
	}
	if err := s.saveFile(path, value); err != nil {
		return "", err
	}
	if err := s.Legacy.Delete(id); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return value, nil
}

func (s *FileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if err := s.checkDirectories(path); err != nil {
		return err
	}
	err = os.Remove(path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if s.Legacy != nil {
		return s.Legacy.Delete(id)
	}
	return nil
}
