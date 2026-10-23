package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ConfigureInputResolver resolves only A's explicitly referenced resident
// uploads/artifacts. Worker refs always go through this Manager's ownership check.
func (m *Manager) ConfigureInputResolver(resolve func(context.Context, string) (api.WorkFileContent, error)) {
	m.inputResolver = resolve
}
func (m *Manager) validateWorkerInputs(ctx context.Context, id string, files []api.InputFile) error {
	if len(files) == 0 {
		return nil
	}
	if p, ok := m.work.(api.WorkInputValidator); ok {
		return p.ValidateWorkFiles(ctx, id, files)
	}
	return nil // Legacy embedders keep their existing native checks.
}
func (m *Manager) rejectStartInput(request string, cause error) (api.Task, error) {
	// A validation failure proves nothing about an earlier dispatch of this ID.
	// Serialize with admission before consulting the existing product records.
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, records := range []map[string]*record{m.state.Records, m.quarantined} {
		for _, r := range records {
			if r != nil && slices.Contains(r.Requests, request) {
				v := r.View
				v.Outcome = "unknown"
				return v, cause
			}
		}
	}
	return api.Task{Outcome: "rejected"}, &api.WorkMessageRejection{Cause: cause}
}
func (m *Manager) inputFiles(ctx context.Context, refs []api.TaskFileReference, files []api.InputFile) ([]api.InputFile, error) {
	if len(refs) == 0 {
		return files, nil
	}
	if len(refs)+len(files) > 8 {
		return nil, errors.New("too_many_worker_files")
	}
	out := append([]api.InputFile(nil), files...)
	for _, ref := range refs {
		var v api.WorkFileContent
		var err error
		if ref.TaskID != "" {
			var copy api.TaskFileCopy
			copy, err = m.MaterializeTaskFile(ctx, ref.TaskID, ref.ID, false)
			if err == nil {
				v.Name, v.MediaType = copy.Name, copy.MediaType
				v.Data, err = os.ReadFile(copy.Path)
				if err == nil {
					sum := sha256.Sum256(v.Data)
					if int64(len(v.Data)) != copy.Bytes || hex.EncodeToString(sum[:]) != copy.SHA256 {
						err = errors.New("worker_input_copy_changed")
					}
				}
			}
		} else if m.inputResolver != nil {
			v, err = m.inputResolver(ctx, ref.ID)
		} else {
			err = errors.New("resident_attachment_unavailable")
		}
		if err != nil {
			return nil, err
		}
		if v.Name == "" || v.Name != filepath.Base(v.Name) || strings.ContainsAny(v.Name, "/\\\x00") || len(v.Data) > api.MaxWorkFileBytes {
			return nil, errors.New("invalid_worker_file")
		}
		dir := filepath.Join(m.root, ".inputs", api.WorkFingerprint(struct {
			Name string
			Data []byte
		}{v.Name, v.Data}))
		path := filepath.Join(dir, v.Name)
		m.inputMu.Lock()
		for _, directory := range []string{m.root, filepath.Join(m.root, ".inputs"), dir} {
			if err = localstate.EnsurePrivateDirectory(directory); err != nil {
				break
			}
			info, e := os.Lstat(directory)
			if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				err = errors.New("worker_input_copy_unavailable")
				break
			}
		}
		if err == nil {
			_, statErr := os.Lstat(path)
			err = writeInputCopy(path, v.Data)
			if os.IsNotExist(statErr) {
				if info, e := os.Lstat(path); e == nil && info.Mode().IsRegular() {
					if m.inputCreated == nil {
						m.inputCreated = map[string]os.FileInfo{}
					}
					m.inputCreated[path] = info
				}
			}
		}
		if err != nil {
			if root, e := os.OpenRoot(filepath.Join(m.root, ".inputs")); e == nil {
				if rel, e := filepath.Rel(filepath.Join(m.root, ".inputs"), dir); e == nil && filepath.IsLocal(rel) {
					_ = root.Remove(rel)
				}
				_ = root.Close()
			}
		}
		m.inputMu.Unlock()
		if err != nil {
			return nil, err
		}
		out = append(out, api.InputFile{Name: v.Name, Path: path})
	}
	return out, nil
}

// Cache content is immutable while a native submission may still read it.
func writeInputCopy(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("worker_input_copy_unavailable")
	}
	if prior, err := os.ReadFile(path); err == nil {
		if string(prior) != string(data) {
			return errors.New("worker_input_copy_changed")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".input-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	return localstate.SyncParent(path)
}
func messageFingerprint(in api.TaskMessage) string {
	if len(in.FileDigests) == 0 && len(in.Attachments) == 0 {
		return hash(in.Prompt)
	}
	return api.WorkFingerprint(struct {
		Prompt string
		Files  []string
		Refs   []api.TaskFileReference
	}{in.Prompt, in.FileDigests, in.Attachments})
}
