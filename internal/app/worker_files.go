package app

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"io"
	"os"
	"path/filepath"
)

func (a *Application) configureTaskFiles(m *tasks.Manager) {
	if m == nil {
		return
	}
	m.ConfigureInputResolver(a.residentWorkerFile)
	a.Backend.ConfigureWorkerTaskView(m)
	a.Backend.ConfigureWorkerFiles(filepath.Join(a.root, "WorkerFiles"), m)
}
func (a *Application) residentWorkerFile(ctx context.Context, id string) (api.WorkFileContent, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkFileContent{}, err
	}
	engine := a.currentEngine()
	owned := false
	for _, item := range engine.Snapshot().Items {
		for _, ref := range item.Artifacts {
			if ref.ID == id {
				owned = true
			}
		}
	}
	if !owned {
		return api.WorkFileContent{}, errors.New("resident_file_not_owned")
	}
	resolver, ok := engine.(api.ArtifactResolver)
	if !ok {
		return api.WorkFileContent{}, errors.New("resident_file_unavailable")
	}
	path, err := resolver.Artifact(id)
	if err != nil {
		return api.WorkFileContent{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return api.WorkFileContent{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, api.MaxWorkFileBytes+1))
	if err != nil || len(data) > api.MaxWorkFileBytes {
		return api.WorkFileContent{}, errors.New("worker_file_too_large")
	}
	return api.WorkFileContent{Name: filepath.Base(path), Data: data}, nil
}
