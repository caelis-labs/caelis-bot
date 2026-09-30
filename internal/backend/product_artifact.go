package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// ReadProductArtifact resolves a current product handle to an exact task-owned
// Worker resource. It accepts neither native identifiers nor filesystem paths.
func (s *Service) ReadProductArtifact(ctx context.Context, id string) (api.WorkArtifact, error) {
	w := s.workerInteractions()
	if w == nil || !isWorkerArtifact(id) {
		return api.WorkArtifact{}, errors.New("product artifact is unavailable")
	}
	w.project(api.Snapshot{})
	w.mu.Lock()
	binding, exists := w.artifacts[id]
	w.mu.Unlock()
	if !exists || !w.owner.OwnsWorkTarget(binding.ref.TaskID, binding.ref.Target) {
		return api.WorkArtifact{}, errors.New("worker artifact changed or is no longer owned")
	}
	artifact, err := binding.provider.ReadWorkArtifact(ctx, binding.ref.TaskID, binding.ref.Artifact.ID)
	if err != nil {
		return api.WorkArtifact{}, errors.New("worker artifact could not be downloaded from its original task")
	}
	if artifact.ID != binding.ref.Artifact.ID || artifact.Name != binding.ref.Artifact.Name || artifact.Name == "" || len(artifact.Name) > 255 || artifact.Name != filepath.Base(artifact.Name) || artifact.Name == "." || artifact.Name == ".." || strings.ContainsAny(artifact.Name, "\x00/\\\r\n") || artifact.Size < 0 || artifact.Size > api.MaxWorkArtifactBytes || artifact.Size != int64(len(artifact.Bytes)) {
		return api.WorkArtifact{}, errors.New("worker artifact metadata is invalid")
	}
	sum := sha256.Sum256(artifact.Bytes)
	if artifact.SHA256 != hex.EncodeToString(sum[:]) {
		return api.WorkArtifact{}, errors.New("worker artifact checksum does not match")
	}
	// A detached/replaced route cannot retain authority while a download is in flight.
	w.project(api.Snapshot{})
	w.mu.Lock()
	current, active := w.artifacts[id]
	w.mu.Unlock()
	if !active || current.ref != binding.ref || !w.owner.OwnsWorkTarget(binding.ref.TaskID, binding.ref.Target) {
		return api.WorkArtifact{}, errors.New("worker artifact changed or is no longer owned")
	}
	return artifact, nil
}
