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
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const inputReferencePrefix = "upload-file:"

// ErrInputConflict refuses a changed intent under an existing submission ID.
var ErrInputConflict = errors.New("original upload submission has different input")

// ErrInputUnavailable reports unavailable or unverified retained input bytes.
var ErrInputUnavailable = errors.New("retained upload is unavailable")
var inputGates sync.Map // Reopening the same App cache preserves one publication lock.

// Inputs retains only explicitly selected user file bytes. Its manifests bind
// immutable content to one original submission; they never record an execution
// outcome or authorize a native submission.
type Inputs struct {
	mu   *sync.Mutex
	root string
}

// InputReference is host-owned metadata supplied to the resident Agent. Name is
// untrusted user data; ID is resolved only through the private manifest.
type InputReference struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type inputManifest struct {
	Version int              `json:"version"`
	Key     string           `json:"key"`
	Input   string           `json:"input"`
	Intent  string           `json:"intent"`
	Files   []InputReference `json:"files"`
}

// OpenInputs prepares a private file cache without touching native state.
func OpenInputs(root string) (*Inputs, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, ErrInputUnavailable
	}
	for _, path := range []string{root, filepath.Join(root, "manifests"), filepath.Join(root, "bytes")} {
		if err := localstate.EnsurePrivateDirectory(path); err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrInputUnavailable
		}
		if err := localstate.ProtectDirectory(path); err != nil {
			return nil, err
		}
	}
	gate, _ := inputGates.LoadOrStore(filepath.Clean(root), new(sync.Mutex))
	return &Inputs{root: root, mu: gate.(*sync.Mutex)}, nil
}

func inputHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func inputHex(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}
func inputName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 1024 && utf8.ValidString(name) && name == filepath.Base(name) && !strings.ContainsAny(name, "/\\\x00")
}
func inputReference(key string, index int, digest string) string {
	return inputReferencePrefix + key + ":" + strconv.Itoa(index) + ":" + digest
}

// IsInputReference also recognizes malformed upload IDs so they fail at this
// owner instead of falling through to an unrelated resident artifact resolver.
func IsInputReference(id string) bool { return strings.HasPrefix(id, inputReferencePrefix) }

func parseInputReference(id string) (key string, index int, digest string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(id, inputReferencePrefix), ":")
	if !IsInputReference(id) || len(parts) != 3 || !inputHex(parts[0]) || !inputHex(parts[2]) {
		return "", 0, "", false
	}
	i, err := strconv.Atoi(parts[1])
	if err != nil || i < 0 || i >= 8 || strconv.Itoa(i) != parts[1] {
		return "", 0, "", false
	}
	return parts[0], i, parts[2], true
}

func (s *Inputs) manifest(key string) (inputManifest, error) {
	var out inputManifest
	path := filepath.Join(s.root, "manifests", key+".json")
	info, err := os.Lstat(path)
	if err != nil {
		return out, err
	}
	if !info.Mode().IsRegular() || info.Size() > 32<<10 || !localstate.RestrictedAccess(path, info) {
		return out, ErrInputUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 32<<10+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF || out.Version != 1 || out.Key != key || !inputHex(out.Input) || !inputHex(out.Intent) || len(out.Files) == 0 || len(out.Files) > 8 {
		return inputManifest{}, ErrInputUnavailable
	}
	for i, ref := range out.Files {
		if !inputName(ref.Name) || !inputHex(ref.SHA256) || ref.Size < 0 || ref.Size > api.MaxWorkFileBytes || ref.ID != inputReference(key, i, ref.SHA256) {
			return inputManifest{}, ErrInputUnavailable
		}
	}
	if out.Intent != inputIntent(out.Input, out.Files) {
		return inputManifest{}, ErrInputUnavailable
	}
	return out, nil
}

func inputIntent(input string, files []InputReference) string {
	return api.WorkFingerprint(struct {
		Input string
		Files []InputReference
	}{input, files})
}

func inputBytes(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrInputUnavailable
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() > api.MaxWorkFileBytes {
		return nil, ErrInputUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, api.MaxWorkFileBytes+1))
	after, statErr := file.Stat()
	if err != nil || statErr != nil || len(data) > api.MaxWorkFileBytes || int64(len(data)) != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, ErrInputUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *Inputs) readBytes(ctx context.Context, ref InputReference) ([]byte, error) {
	path := filepath.Join(s.root, "bytes", ref.SHA256)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !localstate.RestrictedAccess(path, info) {
		return nil, ErrInputUnavailable
	}
	data, err := inputBytes(ctx, path)
	if err != nil || int64(len(data)) != ref.Size || inputHash(data) != ref.SHA256 {
		return nil, ErrInputUnavailable
	}
	return data, nil
}

func (s *Inputs) saveBytes(ctx context.Context, ref InputReference, data []byte) error {
	path := filepath.Join(s.root, "bytes", ref.SHA256)
	if _, err := os.Lstat(path); err == nil {
		_, err = s.readBytes(ctx, ref)
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".input-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	err = localstate.ProtectOpenFile(file)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	return localstate.SyncParent(path)
}

// Retain snapshots the selected files before native dispatch. The original
// submission ID, full host model input, ordered names and bytes must all match
// an existing manifest; an uncertain native write never permits changed input.
func (s *Inputs) Retain(ctx context.Context, submission, modelInput string, files []api.InputFile) ([]api.InputFile, []InputReference, error) {
	if submission == "" || len(submission) > 512 || len(files) > 8 {
		return nil, nil, ErrInputUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := inputHash([]byte(submission))
	old, loadErr := s.manifest(key)
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return nil, nil, ErrInputUnavailable
	}
	if len(files) == 0 && errors.Is(loadErr, os.ErrNotExist) {
		return files, nil, nil
	}
	refs := make([]InputReference, 0, len(files))
	prepared := make([]api.InputFile, 0, len(files))
	for index, file := range files {
		if !inputName(file.Name) {
			return nil, nil, ErrInputUnavailable
		}
		data, err := inputBytes(ctx, file.Path)
		if err != nil {
			return nil, nil, err
		}
		digest := inputHash(data)
		ref := InputReference{ID: inputReference(key, index, digest), Name: file.Name, SHA256: digest, Size: int64(len(data))}
		if err := s.saveBytes(ctx, ref, data); err != nil {
			return nil, nil, err
		}
		refs = append(refs, ref)
		prepared = append(prepared, api.InputFile{Name: file.Name, Path: filepath.Join(s.root, "bytes", digest)})
	}
	input := inputHash([]byte(modelInput))
	intent := inputIntent(input, refs)
	if loadErr == nil {
		if old.Intent != intent {
			return nil, nil, ErrInputConflict
		}
		return prepared, refs, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	manifest := inputManifest{Version: 1, Key: key, Input: input, Intent: intent, Files: refs}
	if err := localstate.WriteConfirmed(filepath.Join(s.root, "manifests", key+".json"), manifest); err != nil {
		return nil, nil, err
	}
	return prepared, refs, nil
}

// Read resolves exact retained bytes, never a client path or a bare digest.
func (s *Inputs) Read(ctx context.Context, id string) (api.WorkFileContent, error) {
	key, index, digest, ok := parseInputReference(id)
	if !ok {
		return api.WorkFileContent{}, ErrInputUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.manifest(key)
	if err != nil || index >= len(manifest.Files) || manifest.Files[index].ID != id || manifest.Files[index].SHA256 != digest {
		return api.WorkFileContent{}, ErrInputUnavailable
	}
	ref := manifest.Files[index]
	data, err := s.readBytes(ctx, ref)
	if err != nil {
		return api.WorkFileContent{}, err
	}
	return api.WorkFileContent{Name: ref.Name, Data: data, MediaType: http.DetectContentType(data)}, nil
}
