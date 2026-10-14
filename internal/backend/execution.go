package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

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
func (s *Service) ExecutionSettings() (api.ExecutionSettings, error) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if source, ok := s.currentEngine().(api.ExecutionSettingsSource); ok {
		return source.CurrentExecutionSettings(context.Background())
	}
	return s.executionSettings, nil
}

// BotExecutionMode is the saved choice for a future conversation binding.
// Reading it never reports or mutates the current native Session permissions.
func (s *Service) BotExecutionMode() (string, error) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	return LoadBotExecutionMode(s.executionFile)
}

func botModeFile(executionFile string) string {
	return filepath.Join(filepath.Dir(executionFile), "bot-execution-mode.json")
}

func LoadBotExecutionMode(executionFile string) (string, error) {
	data, err := os.ReadFile(botModeFile(executionFile))
	if errors.Is(err, os.ErrNotExist) {
		return "auto", nil
	}
	if err != nil {
		return "", err
	}
	var stored struct {
		Version int    `json:"version"`
		Mode    string `json:"mode"`
	}
	if json.Unmarshal(data, &stored) != nil || stored.Version != 1 || (stored.Mode != "auto" && stored.Mode != "full-access") {
		return "", errors.New("Bot 执行模式设置无法识别，请保留文件并重试")
	}
	return stored.Mode, nil
}

func (s *Service) SaveBotExecutionMode(mode string) error {
	if mode != "auto" && mode != "full-access" {
		return errors.New("执行模式仅支持自动审查或完全访问")
	}
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if mode == "full-access" {
		provider, ok := s.currentEngine().(api.Provider)
		if !ok || provider.ProviderInfo().ID == "caelis" {
			return errors.New("Caelis 完全访问仍需 Core 提供创建时执行审批合同；当前连接不可选择")
		}
	}
	if _, err := LoadBotExecutionMode(s.executionFile); err != nil {
		return err
	}
	if err := saveRuntimeSettings(botModeFile(s.executionFile), struct {
		Version int    `json:"version"`
		Mode    string `json:"mode"`
	}{1, mode}); err != nil {
		return errors.New("Bot 执行模式未能保存，原有设置保持不变")
	}
	if e, ok := s.currentEngine().(interface{ SetFutureExecutionMode(string) }); ok {
		e.SetFutureExecutionMode(mode)
	}
	return nil
}
func (s *Service) Models(ctx context.Context) ([]api.ModelOption, error) {
	e, ok := s.currentEngine().(api.ExecutionProvider)
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
	e, ok := s.currentEngine().(api.ExecutionProvider)
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

// ClearDeletedModelPreference follows a confirmed shared Host deletion. It
// does not send an empty application-model patch, which the Host rejects; the
// Host has already reconciled its active Session and default model itself.
func (s *Service) ClearDeletedModelPreference(ctx context.Context, deleted string) error {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if s.executionSettings.Model != deleted {
		return nil
	}
	reconciler, ok := s.currentEngine().(interface {
		ReconcileDeletedModel(context.Context, string, func() error) error
	})
	if !ok {
		return errors.New("当前运行时无法核对已移除的模型")
	}
	next := s.executionSettings
	next.Model, next.Effort, next.ServiceTier = "", "", ""
	return reconciler.ReconcileDeletedModel(ctx, deleted, func() error {
		if err := saveRuntimeSettings(s.executionFile, next); err != nil {
			return err
		}
		s.executionSettings = next
		return nil
	})
}

func (s *Service) ExecutionOptions() (api.ExecutionOptions, error) {
	if e, ok := s.currentEngine().(api.ExecutionProvider); ok {
		return e.ExecutionOptions(), nil
	}
	return api.ExecutionOptions{}, errors.New("当前运行时不支持模型设置")
}
