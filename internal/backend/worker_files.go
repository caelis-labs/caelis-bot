package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) ConfigureWorkerFiles(root string, reader api.TaskFileReader) {
	s.workerFileRoot = root
	s.workerFiles = reader
}
func (s *Service) taskArtifact(id string) (string, bool, error) {
	ref, ok := api.ParseTaskArtifactID(id)
	if !ok {
		return "", false, nil
	}
	if s.workerFiles == nil {
		return "", true, errors.New("worker_file_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	v, err := s.workerFiles.ReadTaskFile(ctx, ref.TaskID, ref.ID)
	if err != nil {
		return "", true, err
	}
	if v.Name == "" || v.Name != filepath.Base(v.Name) || strings.ContainsAny(v.Name, "/\\\x00") || len(v.Data) > api.MaxWorkFileBytes {
		return "", true, errors.New("invalid_worker_file")
	}
	dir := filepath.Join(s.workerFileRoot, api.WorkFingerprint(struct {
		Task, ID string
		Data     []byte
	}{ref.TaskID, ref.ID, v.Data}))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", true, err
	}
	path := filepath.Join(dir, v.Name)
	if err = os.WriteFile(path, v.Data, 0600); err != nil {
		return "", true, err
	}
	return path, true, nil
}
