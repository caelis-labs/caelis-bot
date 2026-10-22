package tasks

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"strings"
)

// ConfigureInputResolver resolves only A's explicitly referenced resident
// artifacts. Worker refs always go through this Manager's ownership check.
func (m *Manager) ConfigureInputResolver(resolve func(context.Context, string) (api.WorkFileContent, error)) {
	m.inputResolver = resolve
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
			v, err = m.ReadTaskFile(ctx, ref.TaskID, ref.ID)
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
		if err = os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, v.Name)
		if err = os.WriteFile(path, v.Data, 0600); err != nil {
			return nil, err
		}
		out = append(out, api.InputFile{Name: v.Name, Path: path})
	}
	return out, nil
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
