package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"

	"github.com/caelis-labs/caelis-bot/internal/localstate"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// runtimeDocument accepts the legacy flat document and writes explicit v1.
type runtimeDocument struct {
	Version int `json:"version"`
	api.RuntimeSettings
}

var providerID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func LoadRuntimeSettings(path, defaultProvider string) (api.RuntimeSettings, error) {
	settings := api.RuntimeSettings{Runtime: defaultProvider}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, errors.New("无法读取连接配置")
	}
	var doc runtimeDocument
	if json.Unmarshal(b, &doc) != nil || (doc.Version != 0 && doc.Version != 1) || !providerID.MatchString(doc.Runtime) {
		return settings, errors.New("连接配置无法识别，请保留文件并重试")
	}
	return doc.RuntimeSettings, nil
}
func (s *Service) ConfigureRuntime(path string, settings api.RuntimeSettings) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	s.runtimeFile, s.runtimeSettings = path, settings
}
func (s *Service) RuntimeSettings() api.RuntimeSettings {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	return s.runtimeSettings
}
func (s *Service) SaveRuntimeSettings(ctx context.Context, value api.RuntimeSettings) (api.RuntimeCheck, error) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	// Live provider replacement needs a separate ownership/migration transaction.
	// Never reuse the active engine's bindings for a different provider.
	activeProvider := s.runtimeSettings.Runtime
	if p, ok := s.engine.(api.Provider); ok {
		activeProvider = p.ProviderInfo().ID
	}
	if s.switchGuard != nil {
		if err := s.switchGuard(); err != nil {
			return api.RuntimeCheck{}, err
		}
	}
	if value.Runtime != activeProvider {
		if s.probeRuntime == nil {
			return api.RuntimeCheck{}, errors.New("当前连接不支持切换后端")
		}

		if err := s.probeRuntime(ctx, value); err != nil {
			return api.RuntimeCheck{}, err
		}
		if err := saveRuntimeSettings(s.runtimeFile, runtimeDocument{Version: 1, RuntimeSettings: value}); err != nil {
			return api.RuntimeCheck{}, err
		}
		s.runtimeSettings = value
		return api.RuntimeCheck{Saved: true, Message: "已检测并保存，下次启动切换运行时；当前对话保持原连接。"}, nil
	}
	e, ok := s.engine.(api.RuntimeConfigurator)
	if !ok {
		return api.RuntimeCheck{}, errors.New("当前后端不支持修改连接配置")
	}
	return e.ChangeRuntime(ctx, value, func() error {
		if err := saveRuntimeSettings(s.runtimeFile, runtimeDocument{Version: 1, RuntimeSettings: value}); err != nil {
			return errors.New("检测已通过，但连接配置未能保存")
		}
		s.runtimeSettings = value
		return nil
	})
}
func saveRuntimeSettings(path string, value any) error { return localstate.Write(path, value) }
