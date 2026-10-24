package tasks

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

func (m *Manager) beginInputPreparation(needed bool) func() {
	if !needed {
		return func() {}
	}
	m.inputMu.Lock()
	m.inputActive++
	m.inputMu.Unlock()
	return func() {
		m.inputMu.Lock()
		m.inputActive--
		if m.inputActive == 0 {
			m.reclaimInputPreparationLocked()
		}
		m.inputMu.Unlock()
	}
}

// inputMu is held. Durable accepted/unknown records always retain their files;
// a failed ledger write disables reclamation until the store is reopened.
func (m *Manager) reclaimInputPreparationLocked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := map[string]bool{}
	uncertain := m.fileStorageUncertain
	for _, records := range []map[string]*record{m.state.Records, m.quarantined} {
		for _, record := range records {
			if record == nil || record.FileStorageVersion == 0 {
				uncertain = true
				continue
			}
			for _, paths := range record.InputPaths {
				for _, path := range paths {
					keep[path] = true
				}
			}
		}
	}
	for path, created := range m.inputCreated {
		if !uncertain && !keep[path] && filepath.IsLocal(relativeStagedPath(m.root, path)) {
			rootInfo, rootErr := os.Lstat(filepath.Join(m.root, ".inputs"))
			parent, parentErr := os.Lstat(filepath.Dir(path))
			if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && os.SameFile(created, info) && rootErr == nil && rootInfo.IsDir() && rootInfo.Mode()&os.ModeSymlink == 0 && parentErr == nil && parent.IsDir() && parent.Mode()&os.ModeSymlink == 0 {
				root, err := os.OpenRoot(filepath.Join(m.root, ".inputs"))
				if err == nil {
					rel := relativeStagedPath(m.root, path)
					if root.Remove(rel) == nil {
						_ = root.Remove(filepath.Dir(rel))
					}
					_ = root.Close()
				}
			}
		}
		delete(m.inputCreated, path)
	}
}

func relativeStagedPath(root, path string) string {
	rel, err := filepath.Rel(filepath.Join(root, ".inputs"), path)
	if err != nil {
		return ".."
	}
	return rel
}

// Release cache references only after the existing native outcome proves this
// particular operation rejected. Request IDs, fingerprints and receipts stay.
func (m *Manager) releaseRejectedInputFiles(id, op string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.state.Records[id]
	if r == nil {
		return nil
	}
	paths, refs := r.InputPaths[op], r.InputReferences[op]
	delete(r.InputPaths, op)
	delete(r.InputReferences, op)
	err := m.write()
	if err != nil {
		m.fileStorageUncertain = true
		if len(paths) > 0 {
			r.InputPaths[op] = paths
		}
		if len(refs) > 0 {
			r.InputReferences[op] = refs
		}
	}
	return err
}

func (r *record) retainInputFiles(op string, refs []api.TaskFileReference, files []api.InputFile) {
	if len(refs) != 0 {
		if r.InputReferences == nil {
			r.InputReferences = map[string][]api.TaskFileReference{}
		}
		r.InputReferences[op] = slices.Clone(refs)
	}
	if len(files) != 0 {
		if r.InputPaths == nil {
			r.InputPaths = map[string][]string{}
		}
		for _, file := range files {
			r.InputPaths[op] = append(r.InputPaths[op], file.Path)
		}
	}
}

// WithFileStorage protects existing product records and in-flight preparation
// during host cache maintenance. These references carry no execution authority.
func (m *Manager) WithFileStorage(ctx context.Context, fn func(workerfiles.Protection, []string) (api.AttachmentStorage, error)) (api.AttachmentStorage, error) {
	if err := lockwait.Lock(ctx, &m.op); err != nil {
		return api.AttachmentStorage{}, err
	}
	defer m.op.Unlock()
	if err := lockwait.Lock(ctx, &m.inputMu); err != nil {
		return api.AttachmentStorage{}, err
	}
	defer m.inputMu.Unlock()
	if err := ctx.Err(); err != nil {
		return api.AttachmentStorage{}, err
	}
	keep := workerfiles.Protection{Complete: m.inputActive == 0}
	var paths []string
	m.mu.Lock()
	keep.Complete = keep.Complete && !m.fileStorageUncertain
	for _, records := range []map[string]*record{m.state.Records, m.quarantined} {
		for id, record := range records {
			keep.Tasks = append(keep.Tasks, id)
			if record == nil || record.FileStorageVersion == 0 {
				keep.Complete = false
				continue
			}
			for op, refs := range record.InputReferences {
				keep.Submissions = append(keep.Submissions, op)
				keep.References = append(keep.References, refs...)
			}
			for _, inputs := range record.InputPaths {
				paths = append(paths, inputs...)
			}
		}
	}
	m.mu.Unlock()
	if !keep.Complete {
		paths = append(paths, filepath.Join(m.root, ".inputs"))
	}
	return fn(keep, paths)
}
