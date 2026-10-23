package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
	"slices"
	"testing"
	"time"
)

func TestWorkerStorageFencesSubmissionsAndRetainsPendingNativeIdentities(t *testing.T) {
	engine := &imReceiptEngine{value: api.Snapshot{InputSources: []api.InputSource{{RequestID: "native-active"}}, LastReceipt: api.Receipt{ID: "original-unknown", Outcome: "unknown"}}}
	s := NewService(engine, nil, nil, nil, nil)
	s.pendingDraft = &api.Submission{ID: "pending-original"}
	s.admission.RLock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := WithWorkerFileStorage(ctx, s, t.TempDir(), true, nil, func(workerfiles.Protection, WorkerInputStorage) (api.AttachmentStorage, error) {
		t.Fatal("timed out storage acquired admission later")
		return api.AttachmentStorage{}, nil
	})
	s.admission.RUnlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	_, err = WithWorkerFileStorage(t.Context(), s, t.TempDir(), false, nil, func(keep workerfiles.Protection, _ WorkerInputStorage) (api.AttachmentStorage, error) {
		for _, id := range []string{"pending-original", "native-active", "original-unknown"} {
			if !slices.Contains(keep.Submissions, id) {
				t.Fatal("original input protection omitted", id)
			}
		}
		if keep.Complete {
			t.Fatal("unloaded history accepted as complete")
		}
		return api.AttachmentStorage{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
