package workerfiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Outputs stores checked immutable bytes by content and a task/ref cache index.
// Callers must establish product task ownership before every lookup, offline too.
// It contains no execution, decision, operation or readiness state.
type Outputs struct {
	root    string
	mu      sync.Mutex
	readers map[string]*sync.Mutex
}
type outputRef struct {
	Task, Ref, Name, MediaType, SHA256 string
	Bytes                              int64
	FetchedAt                          int64
}

func OpenOutputs(root string) *Outputs {
	return &Outputs{root: root, readers: map[string]*sync.Mutex{}}
}
func outputSHA(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func validOutputName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00\r\n")
}

// Preserve the native display name as metadata, not as a host filesystem path.
// A name valid on B can be an alternate stream or reserved filename on Windows A.
// Keep ordinary extensions for local preview/default-app selection.
func outputObjectName(name string) string {
	ext := filepath.Ext(name)
	if len(ext) > 16 {
		return "content"
	}
	for _, c := range strings.TrimPrefix(ext, ".") {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return "content"
		}
	}
	if ext == "." {
		ext = ""
	}
	return "content" + ext
}
func (s *Outputs) copied(r outputRef, cached bool) (api.TaskFileCopy, error) {
	hash, hashErr := hex.DecodeString(r.SHA256)
	if !validOutputName(r.Name) || hashErr != nil || len(hash) != 32 || r.Bytes < 0 || r.Bytes > api.MaxWorkFileBytes || r.FetchedAt <= 0 {
		return api.TaskFileCopy{}, errors.New("worker_file_cache_invalid")
	}
	path := filepath.Join(s.root, "objects", r.SHA256, outputObjectName(r.Name))
	before, e := os.Lstat(path)
	if e != nil || !before.Mode().IsRegular() || !localstate.RestrictedAccess(path, before) {
		return api.TaskFileCopy{}, errors.New("worker_file_cache_unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return api.TaskFileCopy{}, errors.New("worker_file_cache_unavailable")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != r.Bytes || !os.SameFile(before, stat) {
		return api.TaskFileCopy{}, errors.New("worker_file_cache_unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(f, api.MaxWorkFileBytes+1))
	if err != nil || int64(len(data)) != r.Bytes || outputSHA(data) != r.SHA256 {
		return api.TaskFileCopy{}, errors.New("worker_file_cache_unavailable")
	}
	return api.TaskFileCopy{ID: api.TaskArtifactID(r.Task, r.Ref), Name: r.Name, MediaType: r.MediaType, SHA256: r.SHA256, Bytes: r.Bytes, Cached: cached, FetchedAt: r.FetchedAt, Path: path}, nil
}
func (s *Outputs) Materialize(ctx context.Context, task, ref string, refresh bool, read func(context.Context) (api.WorkFileContent, error)) (api.TaskFileCopy, error) {
	if err := ctx.Err(); err != nil {
		return api.TaskFileCopy{}, err
	}
	if !filepath.IsAbs(s.root) || task == "" || ref == "" || len(task) > 1024 || len(ref) > 8192 {
		return api.TaskFileCopy{}, errors.New("worker_file_reference_invalid")
	}
	key := api.WorkFingerprint(struct{ Task, Ref string }{task, ref})
	s.mu.Lock()
	gate := s.readers[key]
	if gate == nil {
		gate = &sync.Mutex{}
		s.readers[key] = gate
	}
	s.mu.Unlock()
	gate.Lock()
	defer gate.Unlock()
	index := filepath.Join(s.root, "refs", key+".json")
	var cached outputRef
	b, err := os.ReadFile(index)
	hasCache := err == nil && len(b) < 64<<10 && json.Unmarshal(b, &cached) == nil && cached.Task == task && cached.Ref == ref
	if hasCache && !refresh {
		if v, e := s.copied(cached, true); e == nil {
			return v, nil
		}
	}
	value, readErr := read(ctx)
	if readErr != nil {
		if hasCache {
			if v, e := s.copied(cached, true); e == nil {
				return v, nil
			}
		}
		return api.TaskFileCopy{}, readErr
	}
	if !validOutputName(value.Name) || len(value.Data) > api.MaxWorkFileBytes {
		return api.TaskFileCopy{}, errors.New("invalid_worker_file")
	}
	media := value.MediaType
	if media == "" {
		media = http.DetectContentType(value.Data)
	}
	r := outputRef{Task: task, Ref: ref, Name: value.Name, MediaType: media, SHA256: outputSHA(value.Data), Bytes: int64(len(value.Data)), FetchedAt: time.Now().UnixMilli()}
	dir := filepath.Join(s.root, "objects", r.SHA256)
	if err = localstate.EnsurePrivateDirectory(dir); err != nil {
		return api.TaskFileCopy{}, err
	}
	file, e := os.CreateTemp(dir, ".output-*")
	if e != nil {
		return api.TaskFileCopy{}, e
	}
	defer os.Remove(file.Name())
	err = localstate.ProtectOpenFile(file)
	if err == nil {
		_, err = file.Write(value.Data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = os.Rename(file.Name(), filepath.Join(dir, outputObjectName(r.Name)))
	}
	if err == nil {
		err = localstate.SyncParent(filepath.Join(dir, outputObjectName(r.Name)))
	}
	if err != nil {
		return api.TaskFileCopy{}, err
	}
	if err = localstate.Write(index, r); err != nil {
		return api.TaskFileCopy{}, err
	}
	return s.copied(r, false)
}
