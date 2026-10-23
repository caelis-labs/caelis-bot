package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

type previewWorkerCopyFixture struct {
	files   *workerfiles.Outputs
	offline atomic.Bool
}

func (f *previewWorkerCopyFixture) ReadTaskFile(context.Context, string, string) (api.WorkFileContent, error) {
	if f.offline.Load() {
		return api.WorkFileContent{}, errors.New("fixture original owner offline")
	}
	return api.WorkFileContent{Name: "销售明细.csv", Data: []byte("name,value\n甲,7\n")}, nil
}
func (f *previewWorkerCopyFixture) MaterializeTaskFile(ctx context.Context, task, ref string, refresh bool) (api.TaskFileCopy, error) {
	if task != "owned-task" || ref != "native-file" {
		return api.TaskFileCopy{}, errors.New("fixture file not owned")
	}
	return f.files.Materialize(ctx, task, ref, refresh, func(ctx context.Context) (api.WorkFileContent, error) { return f.ReadTaskFile(ctx, task, ref) })
}

func TestArtifactPreviewUsesVerifiedWorkerNameWithoutRenamingCache(t *testing.T) {
	f := &previewWorkerCopyFixture{files: workerfiles.OpenOutputs(t.TempDir())}
	b := backend.NewService(nil, nil, nil, nil, nil)
	b.ConfigureWorkerFiles(t.TempDir(), f)
	id := api.TaskArtifactID("owned-task", "native-file")
	copy, err := backend.ResolveRemoteArtifactCopy(b, id)
	if err != nil || filepath.Base(copy.Path) != "content.csv" || copy.Name != "销售明细.csv" {
		t.Fatal("fixture did not use an immutable safe-name cache object", copy.Name, err)
	}
	f.offline.Store(true)
	legacyCalls, opened := 0, 0
	var s *Service
	s = &Service{
		resolveArtifact:     func(id string) (string, error) { legacyCalls++; return backend.ResolveRemoteArtifact(b, id) },
		resolveArtifactCopy: func(id string) (api.TaskFileCopy, error) { return backend.ResolveRemoteArtifactCopy(b, id) },
		openArtifactPreview: func() { opened++ },
	}
	if err := s.ShowArtifactPreview(id); err != nil {
		t.Fatal(err)
	}
	selection, err := s.CurrentArtifactPreview()
	if err != nil || selection.ID != id || selection.Name != "销售明细.csv" || opened != 1 || legacyCalls != 0 {
		t.Fatal("desktop title used the cache basename or a second resolver", selection, err, opened, legacyCalls)
	}
	if _, err := s.PreviewArtifact(id); err != nil || legacyCalls != 1 {
		t.Fatal("existing text reader lost the original owned offline bytes", err)
	}
	if err := s.ShowArtifactPreview(api.TaskArtifactID("another-task", "native-file")); err == nil || legacyCalls != 1 || opened != 1 {
		t.Fatal("metadata refusal fell back to a different artifact owner")
	}
}
