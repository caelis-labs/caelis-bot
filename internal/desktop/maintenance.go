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
		return api.AttachmentStorage{}, errors.New("附件存储暂不可用")
	}
	return s.storage()
}
func (s *Service) CleanAttachmentStorage(ctx context.Context) (api.AttachmentStorage, error) {
	if s.cleanStorage == nil {
		return api.AttachmentStorage{}, errors.New("暂时无法清理")
	}
	return s.cleanStorage(ctx)
}
func (s *Service) ExportDiagnostics() (string, error) {
	if !s.exportMu.TryLock() {
		return "", errors.New("请先完成当前导出")
	}
	defer s.exportMu.Unlock()
	if s.diagnosticReport == nil || s.saveDiagnosticPath == nil {
		return "", errors.New("诊断导出暂不可用")
	}
	data, err := s.diagnosticReport()
	if err != nil || !json.Valid(data) {
		return "", errors.New("暂时无法生成诊断报告")
	}
	path, err := s.saveDiagnosticPath()
	if err != nil {
		return "", errors.New("暂时无法选择保存位置")
	}
	if path == "" {
		return "", nil
	}
	if localstate.Write(path, json.RawMessage(data)) != nil {
		return "", errors.New("暂时无法保存诊断报告")
	}
	return "诊断报告已保存，未自动上传。", nil
}
