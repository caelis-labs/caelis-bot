package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type runtimeSetup struct {
	mu      sync.Mutex
	app     *Application
	codex   *codex.Setup
	pending string
	fresh   bool
}

func (a *Application) configureSetup() {
	fresh := true
	for _, name := range []string{"runtime.json", "conversation.json", "bot.json", "setup.json"} {
		if _, e := os.Stat(filepath.Join(a.root, name)); !os.IsNotExist(e) {
			fresh = false
		}
	}
	a.setup = &runtimeSetup{app: a, fresh: fresh, codex: &codex.Setup{}}
	a.Backend.ConfigureSetup(a.setup)
	a.Backend.RequireSetup(!a.HasRuntimeChoice())
}
func (a *Application) NeedsSetup() bool { return a.setup.Overview().Onboarding }
func (a *Application) ProviderDirectory() string {
	p, _ := providerDirectory(a.root, a.engine.(api.Provider).ProviderInfo().ID)
	return p
}
func (s *runtimeSetup) Overview() api.SetupOverview {
	s.mu.Lock()
	defer s.mu.Unlock()
	return api.SetupOverview{Active: s.app.engine.(api.Provider).ProviderInfo().ID, Pending: s.pending, Onboarding: s.fresh}
}
func (s *runtimeSetup) Profile(id string) (api.RuntimeSettings, error) {
	if id != "codex" && id != "caelis" {
		return api.RuntimeSettings{}, errors.New("不支持的运行时")
	}
	path := filepath.Join(s.app.root, "runtime-profiles", id+".json")
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		return backend.LoadRuntimeSettings(path, id)
	}
	active := s.app.Backend.RuntimeSettings()
	if active.Runtime == id {
		return active, nil
	}
	return api.RuntimeSettings{Runtime: id}, nil
}
func (s *runtimeSetup) saveProfile(v api.RuntimeSettings) error {
	return localstate.Write(filepath.Join(s.app.root, "runtime-profiles", v.Runtime+".json"), struct {
		Version int `json:"version"`
		api.RuntimeSettings
	}{1, v})
}
func validSetup(v api.RuntimeSettings) error {
	if v.Runtime != "codex" && v.Runtime != "caelis" {
		return errors.New("不支持的运行时")
	}
	if v.CLIPath != "" && !filepath.IsAbs(v.CLIPath) {
		return errors.New("程序位置需要完整路径")
	}
	if v.CaelisStore != "" && (v.Runtime != "caelis" || !filepath.IsAbs(v.CaelisStore)) {
		return errors.New("数据目录需要完整路径")
	}
	return nil
}
func (s *runtimeSetup) Inspect(ctx context.Context, v api.RuntimeSettings) (api.SetupState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inspect(ctx, v)
}
func (s *runtimeSetup) inspect(ctx context.Context, v api.RuntimeSettings) (api.SetupState, error) {
	out := api.SetupState{Settings: v, State: "missing", Models: []api.SetupChoice{}}
	if e := validSetup(v); e != nil {
		return out, e
	}
	var installation api.RuntimeStatus
	var e error
	if v.Runtime == "codex" {
		installation, e = codex.InspectRuntime(ctx, v.CLIPath)
	} else {
		st, err := caelisruntime.Inspect(ctx, v.CLIPath)
		e = err
		installation = api.RuntimeStatus{Installed: st.Installed, Path: st.Path, Version: st.Version, Message: st.Message}
	}
	if e != nil {
		return out, e
	}
	out.Installation = installation
	if !installation.Installed {
		out.Message = installation.Message
		return out, nil
	}
	if v.Runtime == "codex" {
		out, e = s.codex.Inspect(ctx, installation.Path)
	} else {
		out.State = "incompatible"
		out.Message = caelis.ApplicationAvailability().Error()
	}
	directory, _ := providerDirectory(s.app.root, v.Runtime)
	prefs, pe := backend.LoadExecutionSettings(filepath.Join(directory, "execution.json"), api.ExecutionSettings{})
	if pe != nil {
		return out, pe
	}
	out.SelectedModel = prefs.Model
	if out.SelectedModel == "" {
		for _, m := range out.Models {
			if m.Current {
				out.SelectedModel = m.Value
				break
			}
		}
	}
	out.Settings = v
	out.Installation = installation
	if e == nil && out.State != "incompatible" {
		e = s.saveProfile(v)
	}
	return out, e
}
func (s *runtimeSetup) Catalog(ctx context.Context, r api.SetupRequest) ([]api.SetupChoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := validSetup(r.Settings); e != nil {
		return nil, e
	}
	if r.Settings.Runtime != "caelis" {
		return nil, errors.New("此运行时没有模型服务目录")
	}
	return caelis.SetupCatalog(ctx, r)
}
func (s *runtimeSetup) Apply(ctx context.Context, r api.SetupRequest) (api.SetupState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := validSetup(r.Settings); e != nil {
		return api.SetupState{}, e
	}
	// Installation and credentials may affect an active shared runtime. Do not
	// discard work or unresolved receipts merely because settings is open.
	if r.Action != "cancel-login" && r.Action != "check-update" && r.Settings.Runtime == s.app.engine.(api.Provider).ProviderInfo().ID {
		if e := s.app.guardRuntimeChange(); e != nil {
			return api.SetupState{}, e
		}
	}
	var e error
	message := ""
	switch r.Action {
	case "install", "update", "check-update", "start":
		if r.Settings.Runtime == "caelis" {
			st, err := caelisruntime.Manage(ctx, r.Action, r.Settings.CLIPath, r.Settings.CaelisStore)
			e = err
			message = st.Message
		} else {
			s.codex.Close()
			s.codex = &codex.Setup{}
			st, err := codex.ManageRuntime(ctx, r.Action, r.Settings.CLIPath)
			e = err
			message = st.Message
		}
	case "login", "cancel-login", "api-key", "logout":
		if r.Settings.Runtime != "codex" {
			return api.SetupState{}, errors.New("此登录方式仅适用于 Codex")
		}
		path, err := codex.InspectRuntime(ctx, r.Settings.CLIPath)
		if err != nil {
			return api.SetupState{}, err
		}
		if !path.Installed {
			return api.SetupState{}, errors.New("请先安装 Codex")
		}
		var u string
		u, e = s.codex.Apply(ctx, path.Path, r.Action, r.APIKey)
		if e == nil && u != "" {
			if s.app.host.OpenURL == nil {
				e = errors.New("无法打开浏览器")
			} else {
				e = s.app.host.OpenURL(u)
			}
		}
	case "connect-model", "remove-model":
		if r.Settings.Runtime != "caelis" {
			return api.SetupState{}, errors.New("此模型管理方式仅适用于 Caelis")
		}
		if r.Action == "remove-model" {
			dir, _ := providerDirectory(s.app.root, "caelis")
			preferences, err := backend.LoadExecutionSettings(filepath.Join(dir, "execution.json"), api.ExecutionSettings{})
			if err != nil {
				return api.SetupState{}, err
			}
			if preferences.Model == r.Model {
				return api.SetupState{}, errors.New("请先更改 Bot 使用的模型，再移除此模型")
			}
		}
		e = caelis.ApplySetup(ctx, r)
		if e == nil {
			message = "配置已保存到 Caelis；尚未发起模型调用"
		}
	case "use-model":
		e = s.useModel(ctx, r)
	default:
		e = errors.New("不支持的运行时管理操作")
	}
	r.APIKey = ""
	if e != nil {
		return api.SetupState{}, e
	}
	out, e := s.inspect(ctx, r.Settings)
	if message != "" {
		out.Message = message
	}
	return out, e
}
func (s *runtimeSetup) useModel(ctx context.Context, r api.SetupRequest) error {
	out, e := s.inspect(ctx, r.Settings)
	if e != nil {
		return e
	}
	found := false
	for _, m := range out.Models {
		if m.Value == r.Model && !m.NoAuth {
			found = true
		}
	}
	if !found {
		return errors.New("请选择可用模型")
	}
	if r.Settings.Runtime == "caelis" {
		if e = caelis.ApplySetup(ctx, r); e != nil {
			return e
		}
	}
	dir, _ := providerDirectory(s.app.root, r.Settings.Runtime)
	defaults := api.ExecutionSettings{ApprovalMode: "workspace-write"}
	if r.Settings.Runtime == "codex" {
		defaults.ApprovalMode = "auto"
	}
	preferences, e := backend.LoadExecutionSettings(filepath.Join(dir, "execution.json"), defaults)
	if e != nil {
		return e
	}
	preferences.Model = r.Model
	preferences.ServiceTier = ""
	preferences.Effort = ""
	if r.Settings.Runtime == "codex" {
		models, err := s.codex.Models(ctx, out.Installation.Path)
		if err != nil {
			return err
		}
		for _, m := range models {
			if m.Model == r.Model {
				preferences.Effort = m.DefaultEffort
			}
		}
	}
	if s.app.engine.(api.Provider).ProviderInfo().ID == r.Settings.Runtime && s.app.engine.Snapshot().Connection == "ready" {
		return s.app.Backend.SaveExecutionSettings(ctx, preferences)
	}
	return localstate.Write(filepath.Join(dir, "execution.json"), preferences)
}
func (s *runtimeSetup) Activate(ctx context.Context, v api.RuntimeSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.app.guardRuntimeChange(); e != nil {
		return e
	}
	out, e := s.inspect(ctx, v)
	if e != nil {
		return e
	}
	if out.State != "ready" {
		return errors.New("请先完成运行时连接")
	}
	if v.Runtime == "caelis" {
		return caelis.ApplicationAvailability()
	}
	// Preserve the outgoing path before replacing the active selection.
	if outgoing := s.app.Backend.RuntimeSettings(); outgoing.Runtime != v.Runtime {
		if e = s.saveProfile(outgoing); e != nil {
			return e
		}
	}
	if e = localstate.Write(filepath.Join(s.app.root, "runtime.json"), struct {
		Version int `json:"version"`
		api.RuntimeSettings
	}{1, v}); e != nil {
		return e
	}
	s.pending = v.Runtime
	s.fresh = false
	return nil
}
func (s *runtimeSetup) Dismiss() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := localstate.Write(filepath.Join(s.app.root, "setup.json"), struct {
		Dismissed bool `json:"dismissed"`
	}{true}); e != nil {
		return e
	}
	s.fresh = false
	return nil
}

func (a *Application) HasRuntimeChoice() bool {
	for _, name := range []string{"runtime.json", "conversation.json", "bot.json"} {
		if _, e := os.Stat(filepath.Join(a.root, name)); !os.IsNotExist(e) {
			return true
		}
	}
	return false
}
func (a *Application) PrepareRestart() error { return a.Backend.PrepareRestart(a.guardRuntimeChange) }
