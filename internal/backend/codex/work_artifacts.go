package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Only completed native fileChange projections grant downloadable identities.
// Agent messages, markdown links and shell output never establish file authority.
func rememberWorkerArtifacts(task *taskRecord, turn nativeTurn) bool {
	if task.WorkerSource == nil || task.Thread == "" {
		return false
	}
	changed := false
	for _, item := range turn.Items {
		if item.Type != "fileChange" || item.Status != "completed" {
			continue
		}
		for _, change := range item.Changes {
			path := change.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(task.View.Workspace, path)
			}
			rel, err := filepath.Rel(task.View.Workspace, filepath.Clean(path))
			if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			id := opaque("worker-artifact", task.View.ID, task.Thread, rel)
			if task.WorkerArtifacts == nil {
				task.WorkerArtifacts = map[string]string{}
			}
			if _, exists := task.WorkerArtifacts[id]; !exists && len(task.WorkerArtifacts) < 256 {
				task.WorkerArtifacts[id] = rel
				changed = true
			}
		}
	}
	return changed
}

func (w *WorkerClient) WorkArtifacts() []api.WorkArtifactRef {
	s := w.engine
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []api.WorkArtifactRef
	for _, task := range s.binding.Tasks {
		for id, path := range task.WorkerArtifacts {
			out = append(out, api.WorkArtifactRef{TaskID: task.View.ID, Target: w.target, Artifact: api.Artifact{ID: id, Name: filepath.Base(path)}})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Artifact.ID < out[j].Artifact.ID })
	return out
}

func (w *WorkerClient) ReadWorkArtifact(ctx context.Context, taskID, artifactID string) (api.WorkArtifact, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkArtifact{}, err
	}
	s := w.engine
	s.mu.Lock()
	task := s.binding.Tasks[taskID]
	if task == nil || task.View.Target == nil || *task.View.Target != w.target {
		s.mu.Unlock()
		return api.WorkArtifact{}, errors.New("Worker artifact task is not owned")
	}
	path, exists := task.WorkerArtifacts[artifactID]
	workspace := task.View.Workspace
	s.mu.Unlock()
	if !exists || filepath.IsAbs(path) || path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return api.WorkArtifact{}, errors.New("Worker artifact projection unavailable")
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return api.WorkArtifact{}, err
	}
	defer root.Close()
	file, err := root.Open(path)
	if err != nil {
		return api.WorkArtifact{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > api.MaxWorkArtifactBytes {
		return api.WorkArtifact{}, errors.New("Worker artifact unavailable or too large")
	}
	bytes, err := io.ReadAll(io.LimitReader(file, api.MaxWorkArtifactBytes+1))
	if err != nil || len(bytes) > api.MaxWorkArtifactBytes {
		return api.WorkArtifact{}, errors.New("Worker artifact limit")
	}
	if err = ctx.Err(); err != nil {
		return api.WorkArtifact{}, err
	}
	digest := sha256.Sum256(bytes)
	return api.WorkArtifact{ID: artifactID, Name: filepath.Base(path), MediaType: http.DetectContentType(bytes), Size: int64(len(bytes)), SHA256: hex.EncodeToString(digest[:]), Bytes: bytes}, nil
}

var _ api.WorkArtifactProvider = (*WorkerClient)(nil)
var _ api.WorkArtifactCatalog = (*WorkerClient)(nil)
