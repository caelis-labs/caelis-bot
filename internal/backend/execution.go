package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func LoadExecutionSettings(path string, defaults api.ExecutionSettings) (api.ExecutionSettings, error) {
	v := defaults
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, errors.New("无法读取模型设置")
	}
	if json.Unmarshal(b, &v) != nil {
		return v, errors.New("模型设置无法识别，请保留文件并重试")
	}
	if err = api.ValidateExecutionSettings(v); err != nil {
		return v, err
	}
	return v, nil
}
func (s *Service) ConfigureExecution(path string, v api.ExecutionSettings) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	s.executionFile = path
	s.executionSettings = v
}
func (s *Service) ExecutionSettings() api.ExecutionSettings {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	return s.executionSettings
}
func (s *Service) Models(ctx context.Context) ([]api.ModelOption, error) {
	e, ok := s.engine.(api.ExecutionProvider)
	if !ok {
		return nil, errors.New("当前运行时不支持模型设置")
	}
	return e.Models(ctx)
}
func (s *Service) SaveExecutionSettings(ctx context.Context, v api.ExecutionSettings) error {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if err := api.ValidateExecutionSettings(v); err != nil {
		return err
	}
	e, ok := s.engine.(api.ExecutionProvider)
	if !ok {
		return errors.New("当前运行时不支持模型设置")
	}
	return e.ChangeExecution(ctx, v, func() error {
		if err := saveRuntimeSettings(s.executionFile, v); err != nil {
			return errors.New("模型设置未能保存，原有设置保持不变")
		}
		s.executionSettings = v
		return nil
	})
}

func (s *Service) ExecutionOptions() (api.ExecutionOptions, error) {
	if e, ok := s.engine.(api.ExecutionProvider); ok {
		return e.ExecutionOptions(), nil
	}
	return api.ExecutionOptions{}, errors.New("当前运行时不支持模型设置")
}
