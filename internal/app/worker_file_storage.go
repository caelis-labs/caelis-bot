package app

import (
	"context"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

func addAttachmentStorage(a, b api.AttachmentStorage) api.AttachmentStorage {
	a.Files += b.Files
	a.Bytes += b.Bytes
	a.EligibleFiles += b.EligibleFiles
	a.EligibleBytes += b.EligibleBytes
	a.CanClean = a.CanClean || b.CanClean
	if a.Notice == "" {
		a.Notice = b.Notice
	}
	return a
}

func (a *Application) workerFileStorage(ctx context.Context, clean bool, trash func(string) error) (api.AttachmentStorage, error) {
	a.mu.Lock()
	manager := a.tasks
	a.mu.Unlock()
	return backend.WithWorkerFileStorage(ctx, a.Backend, filepath.Join(a.root, "WorkerInputs"), clean, trash, func(base workerfiles.Protection, inputs backend.WorkerInputStorage) (api.AttachmentStorage, error) {
		if clean {
			// Recheck the existing guard after new resident submissions are fenced.
			if err := a.guardRuntimeChange(); err != nil {
				return api.AttachmentStorage{}, err
			}
		}
		collect := func(keep workerfiles.Protection, paths []string) (api.AttachmentStorage, error) {
			keep.Paths = append(keep.Paths, paths...)
			keep.Complete = keep.Complete && base.Complete
			keep.Submissions = append(keep.Submissions, base.Submissions...)
			keep.References = append(keep.References, base.References...)
			root := filepath.Join(a.root, "Tasks")
			staging := filepath.Join(root, ".inputs")
			if !keep.Complete {
				paths = append(paths, staging)
			}
			outputs := workerfiles.OpenOutputs(filepath.Join(root, ".files"))
			read := func(apply bool) (api.AttachmentStorage, error) {
				in, err := inputs(keep, apply)
				if err != nil {
					return in, err
				}
				out, err := outputs.Storage(ctx, keep, apply, trash)
				in = addAttachmentStorage(in, out)
				if err != nil {
					return in, err
				}
				stage, err := workerfiles.PlainStorage(ctx, staging, paths, apply, trash)
				in = addAttachmentStorage(in, stage)
				if err != nil {
					return in, err
				}
				if a.peer != nil && a.peer.server != nil {
					peer, err := a.peer.server.InputStorage(ctx, apply, trash)
					return addAttachmentStorage(in, peer), err
				}
				return in, nil
			}
			// Verify all namespaces before the first TrashFile effect. Each store
			// rechecks its own files under its maintenance lock before moving them.
			info, err := read(false)
			if err != nil || !clean {
				return info, err
			}
			return read(true)
		}
		if manager == nil {
			return collect(workerfiles.Protection{}, nil)
		}
		return manager.WithFileStorage(ctx, collect)
	})
}
