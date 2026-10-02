package productrpc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const maxStoredFiles = 256
const maxStoredBytes = 512 << 20

// UploadStore owns a finite private catalog. User names never become paths;
// opaque upload IDs remain valid for native receipt recovery across restart.
type UploadStore struct {
	mu      sync.Mutex
	root    string
	entries map[string]Resource
	// Artifacts must resolve an already owned product artifact, never a path.
	Artifacts func(context.Context, string) (Resource, io.ReadCloser, error)
}

func OpenUploads(root string) (*UploadStore, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("upload directory must be absolute")
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(parent); err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("upload parent must be private and not a link")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("upload directory must be private and not a link")
	}
	s := &UploadStore{root: root, entries: make(map[string]Resource)}
	catalog, err := openPrivateFile(filepath.Join(root, "catalog.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(catalog, maxStoredFiles*1024+1))
	err = errors.Join(err, catalog.Close())
	if err != nil {
		return nil, err
	}
	if len(b) > maxStoredFiles*1024 || json.Unmarshal(b, &s.entries) != nil || s.entries == nil || len(s.entries) > maxStoredFiles {
		return nil, errors.New("invalid private upload catalog")
	}
	for id, meta := range s.entries {
		if !identifier.MatchString(id) || meta.ID != id || meta.Size < 0 || meta.Size > MaxResourceBytes || len(meta.SHA256) != 64 {
			return nil, errors.New("invalid private upload entry")
		}
	}
	return s, nil
}

func (s *UploadStore) Upload(ctx context.Context, meta Resource, reader io.Reader) (Resource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return Resource{}, ctx.Err()
	}
	if len(s.entries) >= maxStoredFiles {
		return Resource{}, errors.New("private upload limit reached")
	}
	total := meta.Size
	for _, entry := range s.entries {
		total += entry.Size
	}
	if total > maxStoredBytes {
		return Resource{}, errors.New("private upload byte limit reached")
	}
	f, err := os.CreateTemp(s.root, ".upload-*")
	if err != nil {
		return Resource{}, err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(reader, MaxResourceBytes+1))
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil || n != meta.Size || n > MaxResourceBytes || hex.EncodeToString(h.Sum(nil)) != meta.SHA256 {
		return Resource{}, errors.New("upload integrity failed")
	}
	meta.ID = rand.Text()
	if err = os.Rename(f.Name(), filepath.Join(s.root, meta.ID)); err != nil {
		return Resource{}, err
	}
	s.entries[meta.ID] = meta
	if err = localstate.Write(filepath.Join(s.root, "catalog.json"), s.entries); err != nil {
		delete(s.entries, meta.ID)
		_ = os.Remove(filepath.Join(s.root, meta.ID))
		return Resource{}, err
	}
	return meta, nil
}

func (s *UploadStore) Open(ctx context.Context, id string) (Resource, io.ReadCloser, error) {
	s.mu.Lock()
	meta, ok := s.entries[id]
	s.mu.Unlock()
	if !ok {
		if s.Artifacts != nil {
			return s.Artifacts(ctx, id)
		}
		return Resource{}, nil, ErrUnsupported
	}
	f, err := openPrivateFile(filepath.Join(s.root, id))
	return meta, f, err
}

func (s *UploadStore) Resolve(ids []string) ([]api.InputFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validIDs(ids, 8) {
		return nil, errors.New("invalid input file selection")
	}
	files := make([]api.InputFile, 0, len(ids))
	for _, id := range ids {
		meta, ok := s.entries[id]
		if !ok {
			return nil, errors.New("input file unavailable")
		}
		path := filepath.Join(s.root, id)
		f, err := openPrivateFile(path)
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, MaxResourceBytes+1))
		err = errors.Join(err, f.Close())
		if err != nil || n != meta.Size || hex.EncodeToString(h.Sum(nil)) != meta.SHA256 {
			return nil, errors.New("input file integrity failed")
		}
		files = append(files, api.InputFile{Name: meta.Name, Path: path})
	}
	return files, nil
}

func openPrivateFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private resource must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		f.Close()
		return nil, errors.New("private resource changed")
	}
	return f, nil
}
