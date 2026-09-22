package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func LoadRuntimeSettings(path string) (api.RuntimeSettings, error) {
	settings := api.RuntimeSettings{Runtime: "codex"}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, errors.New("无法读取连接配置")
	}
	if json.Unmarshal(b, &settings) != nil || settings.Runtime != "codex" {
		return settings, errors.New("连接配置无法识别，请保留文件并重试")
	}
	return settings, nil
}
func (s *Service) ConfigureRuntime(path string, settings api.RuntimeSettings, change func(context.Context, string, func() error) (api.RuntimeCheck, error)) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	s.runtimeFile, s.runtimeSettings, s.changeRuntime = path, settings, change
}
func (s *Service) RuntimeSettings() api.RuntimeSettings {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	return s.runtimeSettings
}
func (s *Service) SaveRuntimeSettings(ctx context.Context, value api.RuntimeSettings) (api.RuntimeCheck, error) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if value.Runtime != "codex" || s.changeRuntime == nil {
		return api.RuntimeCheck{}, errors.New("当前只支持 Codex 运行时")
	}
	if value.CLIPath != "" {
		if !filepath.IsAbs(value.CLIPath) {
			return api.RuntimeCheck{}, errors.New("请选择 Codex CLI，或输入它的完整路径")
		}
		value.CLIPath = filepath.Clean(value.CLIPath)
	}
	return s.changeRuntime(ctx, value.CLIPath, func() error {
		if err := saveRuntimeSettings(s.runtimeFile, value); err != nil {
			return errors.New("检测已通过，但连接配置未能保存")
		}
		s.runtimeSettings = value
		return nil
	})
}
func saveRuntimeSettings(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".runtime-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(value)
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err == nil {
		err = closed
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	return err
}
