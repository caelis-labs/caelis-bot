package workerfiles

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

const attachmentRetention = 30 * 24 * time.Hour

var ErrStorageUncertain = errors.New("worker file references could not be verified")
var ErrStorageChanged = errors.New("worker file storage changed during cleanup")

// Protection is a snapshot from existing conversation/task owners, not another
// task ledger. References protect pending/staged copies. Missing reference
// evidence retains input bytes; current output indexes remain available offline.
type Protection struct {
	Submissions []string
	References  []api.TaskFileReference
	Tasks       []string
	Paths       []string
	Complete    bool
}

type storedFile struct {
	path string
	info os.FileInfo
}

type storagePlan struct {
	root       string
	rootInfo   os.FileInfo
	files      map[string]storedFile
	candidates []storedFile // Only known, unreferenced ordinary cache files.
}

func inventory(ctx context.Context, root string) (storagePlan, error) {
	root = filepath.Clean(root)
	p := storagePlan{root: root, files: map[string]storedFile{}}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if !filepath.IsAbs(root) {
		return p, ErrStorageUncertain
	}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return p, ErrStorageUncertain
	}
	p.rootInfo = info
	unsafe := false
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return ErrStorageUncertain
		}
		if entry.Type()&os.ModeSymlink != 0 {
			unsafe = true
			return nil // WalkDir never traverses a symlink.
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			unsafe = true
			return nil
		}
		p.files[path] = storedFile{path, info}
		return nil
	})
	if err == nil && unsafe {
		err = ErrStorageUncertain
	}
	return p, err
}

func (p storagePlan) result() api.AttachmentStorage {
	var out api.AttachmentStorage
	for _, f := range p.files {
		out.Files++
		out.Bytes += f.info.Size()
	}
	for _, f := range p.candidates {
		out.EligibleFiles++
		out.EligibleBytes += f.info.Size()
	}
	out.CanClean = out.EligibleFiles != 0
	return out
}

func (p storagePlan) apply(ctx context.Context, clean bool, trash func(string) error) (api.AttachmentStorage, error) {
	out := p.result()
	if !clean || len(p.candidates) == 0 {
		return out, nil
	}
	if trash == nil {
		return out, errors.New("worker file trash is unavailable")
	}
	for _, f := range p.candidates {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		root, rootErr := os.Lstat(p.root)
		current, err := os.Lstat(f.path)
		if rootErr != nil || root.Mode()&os.ModeSymlink != 0 || !root.IsDir() || !os.SameFile(p.rootInfo, root) || err != nil || !current.Mode().IsRegular() || !os.SameFile(f.info, current) || f.info.Size() != current.Size() || !f.info.ModTime().Equal(current.ModTime()) {
			return out, ErrStorageChanged
		}
		// Check every parent again: the final file may still be the same inode
		// after a cache directory was replaced by an external symlink.
		for parent := filepath.Dir(f.path); parent != p.root; parent = filepath.Dir(parent) {
			info, err := os.Lstat(parent)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return out, ErrStorageChanged
			}
		}
		if err := trash(f.path); err != nil {
			return out, err
		}
		if _, err := os.Lstat(f.path); !errors.Is(err, os.ErrNotExist) {
			return out, ErrStorageChanged
		}
		out.Files--
		out.Bytes -= f.info.Size()
		out.EligibleFiles--
		out.EligibleBytes -= f.info.Size()
	}
	out.CanClean = false
	return out, nil
}

func sortedFiles(files map[string]storedFile) []storedFile {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]storedFile, 0, len(paths))
	for _, path := range paths {
		out = append(out, files[path])
	}
	return out
}

func (s *Inputs) Storage(ctx context.Context, keep Protection, clean bool, trash func(string) error) (api.AttachmentStorage, error) {
	if s == nil {
		return api.AttachmentStorage{}, nil
	}
	if err := lockwait.Lock(ctx, s.mu); err != nil {
		return api.AttachmentStorage{}, err
	}
	defer s.mu.Unlock()
	p, err := inventory(ctx, s.root)
	if err != nil {
		return p.result(), err
	}
	cutoff := time.Now().Add(-attachmentRetention)
	referenced := map[string]bool{}
	submissions := map[string]bool{}
	for _, id := range keep.Submissions {
		submissions[inputHash([]byte(id))] = true
	}
	for _, ref := range keep.References {
		if ref.TaskID == "" {
			if _, _, hash, ok := parseInputReference(ref.ID); ok {
				referenced[hash] = true
			}
		}
	}
	var bytes []storedFile
	for _, file := range sortedFiles(p.files) {
		rel, _ := filepath.Rel(s.root, file.path)
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "manifests":
			if strings.HasPrefix(parts[1], ".state-") {
				if file.info.ModTime().Before(cutoff) {
					bytes = append(bytes, file)
				}
				continue
			}
			key := strings.TrimSuffix(parts[1], ".json")
			if !inputHex(key) || parts[1] != key+".json" {
				return p.result(), ErrStorageUncertain
			}
			manifest, err := s.manifest(key)
			if err != nil {
				return p.result(), ErrStorageUncertain
			}
			// Keep the immutable intent tombstone even after unused bytes retire.
			// A complete owner snapshot is required to retire an indexed body.
			if !keep.Complete || submissions[key] || !file.info.ModTime().Before(cutoff) {
				for _, ref := range manifest.Files {
					referenced[ref.SHA256] = true
				}
			}
		case "bytes":
			if inputHex(parts[1]) || strings.HasPrefix(parts[1], ".input-") {
				bytes = append(bytes, file)
			}
		}
	}
	for _, file := range bytes {
		if keep.Complete && !referenced[filepath.Base(file.path)] && !protectedStoragePath(file.path, keep.Paths) && file.info.ModTime().Before(cutoff) {
			p.candidates = append(p.candidates, file)
		}
	}
	return p.apply(ctx, clean, trash)
}

func (s *Outputs) Storage(ctx context.Context, keep Protection, clean bool, trash func(string) error) (api.AttachmentStorage, error) {
	if s == nil {
		return api.AttachmentStorage{}, nil
	}
	if err := lockwait.WriteLock(ctx, s.maintenance); err != nil {
		return api.AttachmentStorage{}, err
	}
	defer s.maintenance.Unlock()
	p, err := inventory(ctx, s.root)
	if err != nil {
		return p.result(), err
	}
	cutoff := time.Now().Add(-attachmentRetention)
	referenced := map[string]bool{}
	var objects []storedFile
	for _, file := range sortedFiles(p.files) {
		rel, _ := filepath.Rel(s.root, file.path)
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) == 2 && parts[0] == "refs" {
			if strings.HasPrefix(parts[1], ".state-") {
				objects = append(objects, file)
				continue
			}
			var r outputRef
			if file.info.Size() >= 64<<10 {
				return p.result(), ErrStorageUncertain
			}
			data, err := os.ReadFile(file.path)
			key := ""
			if err == nil && len(data) < 64<<10 {
				err = json.Unmarshal(data, &r)
				key = api.WorkFingerprint(struct{ Task, Ref string }{r.Task, r.Ref})
			}
			if err != nil || len(data) >= 64<<10 || r.Task == "" || r.Ref == "" || len(r.Task) > 1024 || len(r.Ref) > 8192 || !validOutputName(r.Name) || !inputHex(r.SHA256) || r.Bytes < 0 || r.Bytes > api.MaxWorkFileBytes || r.FetchedAt <= 0 || parts[1] != key+".json" {
				return p.result(), ErrStorageUncertain
			}
			// Only superseded/unindexed objects retire. A valid current ref
			// stays readable offline for its original task, regardless of age.
			path := filepath.Join(s.root, "objects", r.SHA256, outputObjectName(r.Name))
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() != r.Bytes {
				return p.result(), ErrStorageUncertain
			}
			referenced[path] = true
		} else if len(parts) == 3 && parts[0] == "objects" && inputHex(parts[1]) && (parts[2] == outputObjectName(parts[2]) || strings.HasPrefix(parts[2], ".output-")) {
			objects = append(objects, file)
		}
	}
	for _, file := range objects {
		if keep.Complete && !referenced[file.path] && !protectedStoragePath(file.path, keep.Paths) && file.info.ModTime().Before(cutoff) {
			p.candidates = append(p.candidates, file)
		}
	}
	return p.apply(ctx, clean, trash)
}

func protectedStoragePath(path string, paths []string) bool {
	for _, protected := range paths {
		if !filepath.IsAbs(protected) {
			continue
		}
		protected = filepath.Clean(protected)
		if path == protected || strings.HasPrefix(path, protected+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Called under the input store's publication lock. Only bytes created by this
// failed preparation may be removed. Any committed (even uncertain) manifest
// keeps its bytes; the immutable submission intent is never rolled back.
func (s *Inputs) rollbackUnpublished(created []string) error {
	if len(created) == 0 {
		return nil
	}
	if err := s.checkDirectories(); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "manifests"))
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".state-") {
			continue
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		if !inputHex(key) || entry.Name() != key+".json" {
			return ErrStorageUncertain
		}
		manifest, err := s.manifest(key)
		if err != nil {
			return ErrStorageUncertain
		}
		for _, ref := range manifest.Files {
			keep[ref.SHA256] = true
		}
	}
	for _, path := range created {
		if filepath.Dir(path) != filepath.Join(s.root, "bytes") || !inputHex(filepath.Base(path)) {
			return ErrStorageUncertain
		}
		if keep[filepath.Base(path)] {
			continue
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return ErrStorageUncertain
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

// PlainStorage is for an explicitly owned staging root. Callers fence new
// work and pass all retained task/op paths; no user source directory is scanned.
func PlainStorage(ctx context.Context, root string, protectedPaths []string, clean bool, trash func(string) error) (api.AttachmentStorage, error) {
	p, err := inventory(ctx, root)
	if err != nil {
		return p.result(), err
	}
	cutoff := time.Now().Add(-attachmentRetention)
	for _, file := range sortedFiles(p.files) {
		protected := protectedStoragePath(file.path, protectedPaths)
		if !protected && file.info.ModTime().Before(cutoff) {
			p.candidates = append(p.candidates, file)
		}
	}
	return p.apply(ctx, clean, trash)
}
