package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func LoadWorkExecutionSettings(path string) (api.WorkExecutionSettings, error) {
	var v api.WorkExecutionSettings
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil || json.Unmarshal(b, &v) != nil {
		return v, errors.New("无法读取工作模型设置，请保留文件并重试")
	}
	if err := api.ValidateExecutionSettings(v.Execution()); err != nil {
		return v, err
	}
	if err := api.ValidateNewExecutionMode(v.ApprovalMode); err != nil {
		return v, err
	}
	return v, nil
}

func (s *Service) ConfigureWorkExecution(path string, v api.WorkExecutionSettings) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	s.workExecutionFile, s.workExecutionSettings = path, v
}

func (s *Service) WorkExecutionSettings() api.WorkExecutionSettings {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	return s.workExecutionSettings
}

func (s *Service) SaveWorkExecutionSettings(ctx context.Context, v api.WorkExecutionSettings) error {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if err := api.ValidateNewExecutionMode(v.ApprovalMode); err != nil {
		return err
	}
	// The shared model picker does not own the Worker permission choice.
	if v.ApprovalMode == "" {
		v.ApprovalMode = s.workExecutionSettings.ApprovalMode
	}
	if err := api.ValidateExecutionSettings(v.Execution()); err != nil {
		return err
	}
	if err := api.ValidateNewExecutionMode(v.ApprovalMode); err != nil {
		return err
	}
	e, ok := s.currentEngine().(api.WorkExecutionProvider)
	if !ok {
		return errors.New("当前运行时不支持独立工作模型")
	}
	return e.ChangeWorkExecution(ctx, v, func() error {
		if err := saveRuntimeSettings(s.workExecutionFile, v); err != nil {
			return errors.New("工作模型设置未能保存，原有设置保持不变")
		}
		s.workExecutionSettings = v
		return nil
	})
}
