package desktop

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func (s *Service) AttachmentStorage() (api.AttachmentStorage, error) {
	if s.storage == nil {
		return api.AttachmentStorage{}, errors.New(s.text("native.attachmentStorageUnavailable", nil))
	}
	return s.storage()
}
func (s *Service) CleanAttachmentStorage(ctx context.Context) (api.AttachmentStorage, error) {
	if s.cleanStorage == nil {
		return api.AttachmentStorage{}, errors.New(s.text("native.cleanupUnavailable", nil))
	}
	return s.cleanStorage(ctx)
}
func (s *Service) ExportDiagnostics() (string, error) {
	if !s.exportMu.TryLock() {
		return "", errors.New(s.text("native.exportInProgress", nil))
	}
	defer s.exportMu.Unlock()
	if s.diagnosticReport == nil || s.saveDiagnosticPath == nil {
		return "", errors.New(s.text("native.diagnosticExportUnavailable", nil))
	}
	data, err := s.diagnosticReport()
	if err != nil || !json.Valid(data) {
		return "", errors.New(s.text("native.diagnosticReportFailed", nil))
	}
	path, err := s.saveDiagnosticPath()
	if err != nil {
		return "", errors.New(s.text("native.chooseSaveLocationFailed", nil))
	}
	if path == "" {
		return "", nil
	}
	if localstate.Write(path, json.RawMessage(data)) != nil {
		return "", errors.New(s.text("native.saveDiagnosticReportFailed", nil))
	}
	return s.text("native.diagnosticSaved", nil), nil
}
