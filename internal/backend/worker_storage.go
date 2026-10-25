package backend

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

// WorkerInputStorage is a host-only operation while admission is fenced. The
// caller joins task/staging maintenance under its existing Manager operation lock.
type WorkerInputStorage func(workerfiles.Protection, bool) (api.AttachmentStorage, error)

func WithWorkerFileStorage(ctx context.Context, s *Service, inputRoot string, clean bool, trash func(string) error, fn func(workerfiles.Protection, WorkerInputStorage) (api.AttachmentStorage, error)) (api.AttachmentStorage, error) {
	if err := lockwait.WriteLock(ctx, &s.admission); err != nil {
		return api.AttachmentStorage{}, err
	}
	defer s.admission.Unlock()
	if err := ctx.Err(); err != nil {
		return api.AttachmentStorage{}, err
	}
	s.mu.Lock()
	store, storeErr, chat := s.workerInputs, s.workerInputsError, s.chat
	keep := workerfiles.Protection{}
	addReferences := func(ids []string) {
		for _, id := range ids {
			if ref, ok := api.ParseTaskArtifactID(id); ok {
				keep.References = append(keep.References, ref)
			} else if workerfiles.IsInputReference(id) {
				keep.References = append(keep.References, api.TaskFileReference{ID: id})
			}
		}
	}
	addReferences(s.draft.ReferenceIDs)
	if s.pendingDraft != nil {
		keep.Submissions = append(keep.Submissions, s.pendingDraft.ID)
		addReferences(s.pendingDraft.ReferenceIDs)
	}
	if s.draftSend != nil {
		keep.Submissions = append(keep.Submissions, s.draftSend.ID)
	}
	for _, pending := range s.outbox {
		keep.Submissions = append(keep.Submissions, pending.item.RequestID)
	}
	s.mu.Unlock()
	if chat != nil {
		refs, err := chat.WorkerFileReferences(ctx)
		keep.Submissions = append(keep.Submissions, refs.Submissions...)
		keep.References = append(keep.References, refs.References...)
		keep.Complete = refs.Complete && err == nil
	}
	if engine := s.currentEngine(); engine != nil {
		v := engine.Snapshot()
		for _, source := range v.InputSources {
			keep.Submissions = append(keep.Submissions, source.RequestID)
		}
		if v.LastReceipt.Outcome == "unknown" {
			keep.Submissions = append(keep.Submissions, v.LastReceipt.ID)
		}
	}
	inputs := func(protection workerfiles.Protection, apply bool) (api.AttachmentStorage, error) {
		if storeErr != nil {
			return api.AttachmentStorage{}, storeErr
		}
		if store == nil {
			// Before Runtime startup there is no configured cache owner yet.
			// Count existing bytes without creating directories or retiring them.
			return workerfiles.PlainStorage(ctx, inputRoot, []string{inputRoot}, false, nil)
		}
		return store.Storage(ctx, protection, clean && apply, trash)
	}
	return fn(keep, inputs)
}
