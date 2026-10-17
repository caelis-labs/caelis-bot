package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

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

// Renderer diagnostics accept only product-owned classifications, never error
// prose, a stack containing private data, paths, credentials or conversation text.
func (s *Service) ReportPresentationWarning(code string) {
	switch code {
	case "surface_render_failed", "bubble_action_failed", "background_read_failed", "setup_activation_failed":
	default:
		return
	}
	s.mu.Lock()
	if s.presentationWarnings == nil {
		s.presentationWarnings = map[string]bool{}
	}
	first := !s.presentationWarnings[code]
	s.presentationWarnings[code] = true
	report := s.presentationWarning
	s.mu.Unlock()
	if first && report != nil {
		report(code)
	}
}

func (s *Service) CopyDiagnostics() error {
	if s.diagnosticReport == nil || s.copyText == nil {
		return errors.New("diagnostics unavailable")
	}
	data, err := s.diagnosticReport()
	if err != nil || !json.Valid(data) || len(data) > 1024*1024 {
		return errors.New("diagnostics unavailable")
	}
	s.mu.Lock()
	codes := make([]string, 0, len(s.presentationWarnings))
	for code := range s.presentationWarnings {
		codes = append(codes, code)
	}
	s.mu.Unlock()
	slices.Sort(codes)
	feedback, err := json.MarshalIndent(struct {
		Product      json.RawMessage `json:"product"`
		Presentation []string        `json:"presentationWarnings"`
	}{data, codes}, "", "  ")
	if err != nil {
		return errors.New("diagnostics unavailable")
	}
	return s.CopyText(string(feedback))
}
