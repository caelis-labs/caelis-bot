package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/updates"
)

type runtimeSetup struct {
	mu          sync.Mutex
	progressMu  sync.Mutex
	progress    api.SetupInstallProgress
	app         *Application
	codex       *codex.Setup
	connections caelis.Connections
	pending     string
	fresh       bool
}

func (s *runtimeSetup) InstallProgress() api.SetupInstallProgress {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	return s.progress
}
func (s *runtimeSetup) setInstallProgress(p caelisruntime.InstallProgress) {
	s.progressMu.Lock()
	s.progress = api.SetupInstallProgress{Phase: p.Phase, Downloaded: p.Downloaded, Total: p.Total, BytesPerSecond: p.BytesPerSecond}
	s.progressMu.Unlock()
}

func (a *Application) configureSetup() {
	fresh := !a.HasRuntimeChoice()
	if _, e := os.Stat(filepath.Join(a.root, "setup.json")); !os.IsNotExist(e) {
		fresh = false
	}
	staged := ""
	if !a.HasRuntimeChoice() {
		var stageErr error
		staged, stageErr = readStagedRuntime(a.root)
		if stageErr != nil && a.host.ReportError != nil {
			a.host.ReportError(stageErr)
		}
	}
	a.setup = &runtimeSetup{app: a, fresh: fresh, pending: staged, codex: &codex.Setup{}}
	a.Backend.ConfigureSetup(a.setup)
	a.Backend.RequireSetup(!a.HasRuntimeChoice())
}
func (s *runtimeSetup) StageRuntime(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != "caelis" && id != "codex" {
		return false, errors.New(s.app.text("unsupportedRuntime", nil))
	}
	if s.app.HasRuntimeChoice() {
		return false, errors.New(s.app.text("unsupportedRuntimeOperation", nil))
	}
	if e := s.app.guardRuntimeChange(); e != nil {
		return false, e
	}
	if e := writeStagedRuntime(s.app.root, id); e != nil {
		return false, e
	}
	s.pending = id
	return s.app.engine.(api.Provider).ProviderInfo().ID != id, nil
}
func (a *Application) NeedsSetup() bool {
	return a.initialization.Initialization().Required || a.setup.Overview().Onboarding
}
func (a *Application) ProviderDirectory() string {
	p, _ := providerDirectory(a.root, a.engine.(api.Provider).ProviderInfo().ID)
	return p
}
func (s *runtimeSetup) Overview() api.SetupOverview {
	s.mu.Lock()
	defer s.mu.Unlock()
	return api.SetupOverview{Active: s.app.engine.(api.Provider).ProviderInfo().ID, HasRuntimeChoice: s.app.HasRuntimeChoice(), Pending: s.pending, Onboarding: s.fresh}
}
func (s *runtimeSetup) Profile(id string) (api.RuntimeSettings, error) {
	if id != "codex" && id != "caelis" {
		return api.RuntimeSettings{}, errors.New(s.app.text("unsupportedRuntime", nil))
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
func validSetup(v api.RuntimeSettings, locale ...i18n.Locale) error {
	loc := i18n.English
	if len(locale) > 0 {
		loc = locale[0]
	}
	if v.Runtime != "codex" && v.Runtime != "caelis" {
		return errors.New(i18n.Text(loc, "host.unsupportedRuntime", nil))
	}
	if v.CLIPath != "" && !filepath.IsAbs(v.CLIPath) {
		return errors.New(i18n.Text(loc, "host.programLocationRequiresFullPath", nil))
	}
	if v.CaelisStore != "" && (v.Runtime != "caelis" || !filepath.IsAbs(v.CaelisStore)) {
		return errors.New(i18n.Text(loc, "host.dataDirectoryRequiresFullPath", nil))
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
		st, err := caelisruntime.Inspect(ctx, v.CLIPath, s.app.locale())
		e = err
		installation = runtimeInstallation(st)
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
		if active, ok := s.app.engine.(*codex.Session); ok && s.app.engine.(api.Provider).ProviderInfo().ID == "codex" {
			if auth, userAgent, shared, detailErr := active.SharedConnectionDetails(ctx); shared {
				out.ServiceVersion = userAgent
				if detailErr == nil {
					out.CLIAccountType = out.AccountType
					out.AccountType = auth.AccountType
					out.AccountMismatch = out.CLIAccountType != out.AccountType
					if auth.RequiresOpenAIAuth && !auth.AccountPresent {
						out.State = "login"
					}
					if out.AccountMismatch {
						out.Message = "当前共享 Codex 服务与所选 CLI 的登录方式不同；Bot 使用共享服务，请在提供该服务的应用中核对登录后重新连接。"
					}
				} else {
					out.Message = "暂时无法读取当前共享 Codex 服务的账号；请重新连接后核对。"
				}
			}
		}
	} else {
		out, e = caelis.InspectSetup(ctx, v)
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
	if order, err := updates.CompareVersions(installation.Version, out.ServiceVersion); err == nil {
		out.ServiceUpdateAvailable = order > 0
	}
	if e == nil && out.State != "incompatible" {
		e = s.saveProfile(v)
	}
	return out, e
}
func (s *runtimeSetup) Catalog(ctx context.Context, r api.SetupRequest) ([]api.SetupChoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := validSetup(r.Settings, s.app.locale()); e != nil {
		return nil, e
	}
	if r.Settings.Runtime != "caelis" {
		return nil, errors.New(s.app.text("noModelServiceDirectory", nil))
	}
	return caelis.SetupCatalog(ctx, r)
}
func (s *runtimeSetup) Apply(ctx context.Context, r api.SetupRequest) (api.SetupState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := validSetup(r.Settings, s.app.locale()); e != nil {
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
	var managed *api.RuntimeStatus
	switch r.Action {
	case "install", "update", "check-update", "start", "apply-update":
		if r.Settings.Runtime == "caelis" {
			if r.Action == "install" {
				s.setInstallProgress(caelisruntime.InstallProgress{Phase: "checking"})
				ctx = caelisruntime.WithInstallProgress(ctx, s.setInstallProgress)
			}
			st, err := s.app.manageCaelis(ctx, r.Action, r.Settings)
			managed = &st
			e = err
			message = st.Message
		} else {
			s.codex.Close()
			s.codex = &codex.Setup{}
			st, err := codex.ManageRuntime(ctx, r.Action, r.Settings.CLIPath)
			managed = &st
			e = err
			message = st.Message
		}
	case "login", "cancel-login", "api-key", "logout":
		if r.Settings.Runtime != "codex" {
			return api.SetupState{}, errors.New(s.app.text("loginOnlyForCodex", nil))
		}
		path, err := codex.InspectRuntime(ctx, r.Settings.CLIPath)
		if err != nil {
			return api.SetupState{}, err
		}
		if !path.Installed {
			return api.SetupState{}, errors.New(s.app.text("installCodexFirst", nil))
		}
		var u string
		u, e = s.codex.Apply(ctx, path.Path, r.Action, r.APIKey)
		if e == nil && u != "" {
			if s.app.host.OpenURL == nil {
				e = errors.New(s.app.text("cannotOpenBrowser", nil))
			} else {
				e = s.app.host.OpenURL(u)
			}
		}
	case "connect-model", "remove-model":
		if r.Settings.Runtime != "caelis" {
			return api.SetupState{}, errors.New(s.app.text("modelManagementOnlyForCaelis", nil))
		}
		if r.Action == "remove-model" {
			dir, _ := providerDirectory(s.app.root, "caelis")
			preferences, err := backend.LoadExecutionSettings(filepath.Join(dir, "execution.json"), api.ExecutionSettings{})
			if err != nil {
				return api.SetupState{}, err
			}
			if preferences.Model == r.Model {
				return api.SetupState{}, errors.New(s.app.text("changeBotModelBeforeRemove", nil))
			}
		}
		e = caelis.ApplySetup(ctx, r)
		if e == nil {
			message = s.app.text("configSavedToCaelisNoCalls", nil)
		}
	case "use-model":
		e = s.useModel(ctx, r)
	default:
		e = errors.New(s.app.text("unsupportedRuntimeOperation", nil))
	}
	r.APIKey = ""
	if e != nil {
		return api.SetupState{}, e
	}
	out, e := s.inspect(ctx, r.Settings)
	if managed != nil {
		out.Installation.LatestVersion = managed.LatestVersion
		out.Installation.UpdateState = managed.UpdateState
	}
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
		return errors.New(s.app.text("selectAvailableModel", nil))
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
		return errors.New(s.app.text("completeRuntimeConnectionFirst", nil))
	}
	// On a fresh default-path setup the chosen engine was constructed before
	// credentials/models were configured. There is no prior conversation or
	// provider binding to migrate, so admission can open in this process.
	// Custom executable/Store changes and established installations retain the
	// restart transaction until their live binding can be moved safely.
	if s.fresh && v.Runtime == s.app.engine.(api.Provider).ProviderInfo().ID && v.CLIPath == "" && v.CaelisStore == "" {
		if _, e = s.app.Backend.SaveRuntimeSettings(ctx, v); e != nil {
			return e
		}
		s.app.Backend.RequireSetup(false)
		if e = s.app.Start(); e != nil {
			return e
		}
		if e = s.app.Backend.Connect(ctx); e != nil {
			return e
		}
		s.clearStagedRuntime()
		s.pending = ""
		s.fresh = false
		return nil
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
	s.clearStagedRuntime()
	return nil
}
func (s *runtimeSetup) clearStagedRuntime() {
	if err := os.Remove(filepath.Join(s.app.root, stagedRuntimeFile)); err != nil && !errors.Is(err, os.ErrNotExist) && s.app.host.ReportError != nil {
		s.app.host.ReportError(err)
	}
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
	if _, e := os.Stat(filepath.Join(a.root, "runtime.json")); !os.IsNotExist(e) {
		return true
	}
	// Only pre-Notebook identities imply the historical Codex default. Creating
	// personal data or a conversation journal offline must not silently choose a
	// Runtime on next launch.
	var legacy struct {
		Version         int `json:"version"`
		PersonalVersion int `json:"personalVersion"`
	}
	b, e := os.ReadFile(filepath.Join(a.root, "bot.json"))
	if e == nil && json.Unmarshal(b, &legacy) == nil && legacy.Version == 1 {
		if legacy.PersonalVersion != 0 {
			return false
		}
		return true
	}
	_, e = os.Stat(filepath.Join(a.root, "conversation.json"))
	return e == nil
}
func (a *Application) PrepareRestart() error { return a.Backend.PrepareRestart(a.guardRuntimeChange) }
