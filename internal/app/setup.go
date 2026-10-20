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
	progressMu        sync.Mutex
	progress          api.SetupInstallProgress
	operationMu       sync.Mutex
	operationCancel   context.CancelFunc
	mu                sync.Mutex
	app               *Application
	codex             *codex.Setup
	connections       caelis.Connections
	pending           string
	fresh             bool
	prepared          map[string]*setupPreparation
	activePreparation *setupPreparation
	activeEngine      api.Engine
	reconnectRequired bool
}

func (s *runtimeSetup) InstallProgress() api.SetupInstallProgress {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	return s.progress
}
func (s *runtimeSetup) setInstallProgress(p caelisruntime.InstallProgress) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	s.progress = api.SetupInstallProgress{Phase: p.Phase, Downloaded: p.Downloaded, Total: p.Total, BytesPerSecond: p.BytesPerSecond}
}
func (s *runtimeSetup) CancelOperation() bool {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.operationCancel == nil {
		return false
	}
	s.operationCancel()
	return true
}

func (a *Application) configureSetup() {
	fresh := !a.HasRuntimeChoice()
	if _, e := os.Stat(filepath.Join(a.root, "setup.json")); !os.IsNotExist(e) {
		fresh = false
	}
	a.setup = &runtimeSetup{app: a, fresh: fresh, codex: &codex.Setup{}}
	a.Backend.ConfigureSetup(a.setup)
	a.Backend.RequireSetup(!a.HasRuntimeChoice())
}
func (a *Application) NeedsSetup() bool {
	return a.initialization.Initialization().Required || a.setup.Overview().Onboarding
}
func (a *Application) ProviderDirectory() string {
	p, _ := providerDirectory(a.root, a.currentEngine().(api.Provider).ProviderInfo().ID)
	return p
}
func (s *runtimeSetup) Overview() api.SetupOverview {
	s.mu.Lock()
	defer s.mu.Unlock()
	return api.SetupOverview{Active: s.app.currentEngine().(api.Provider).ProviderInfo().ID, HasRuntimeChoice: s.app.HasRuntimeChoice(), Pending: s.pending, Onboarding: s.fresh}
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
	delete(s.prepared, v.Runtime) // Explicit recheck always observes the Runtime.
	return s.inspect(ctx, v)
}
func (s *runtimeSetup) inspect(ctx context.Context, v api.RuntimeSettings) (out api.SetupState, err error) {
	defer func() {
		if err == nil {
			s.rememberPreparation(ctx, out)
			// Reuse the successful inspection; settings must not launch another
			// probe just to reconcile the default worker model.
			if out.State == "ready" && s.app.localWork != nil {
				models := []api.ModelOption{}
				complete := true
				for _, choice := range out.Models {
					if choice.NoAuth {
						continue
					}
					if choice.Parameters == nil {
						complete = false
						break
					}
					models = append(models, *choice.Parameters)
				}
				if complete {
					s.reconcileWorkerModels(v.Runtime, models)
				}
			}
		}
	}()
	out = api.SetupState{Settings: v, State: "missing", Models: []api.SetupChoice{}}
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
	if r.Action == "install" || r.Action == "update" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		s.operationMu.Lock()
		s.operationCancel = cancel
		s.operationMu.Unlock()
		defer func() { s.operationMu.Lock(); s.operationCancel = nil; s.operationMu.Unlock(); cancel() }()
		s.setInstallProgress(caelisruntime.InstallProgress{Phase: "checking"})
		ctx = caelisruntime.WithInstallProgress(ctx, s.setInstallProgress)
	}

	if e := validSetup(r.Settings, s.app.locale()); e != nil {
		return api.SetupState{}, e
	}
	if r.Action == "prepare" {
		return s.prepare(ctx, r.Settings)
	}
	// Installation and credentials may affect an active shared runtime. Do not
	// discard work or unresolved receipts merely because settings is open.
	if r.Action != "use-model" && r.Action != "cancel-login" && r.Action != "check-update" && r.Settings.Runtime == s.app.currentEngine().(api.Provider).ProviderInfo().ID {
		if e := s.app.guardRuntimeChange(); e != nil {
			return api.SetupState{}, e
		}
	}
	if r.Action == "use-model" {
		return s.selectModel(ctx, r)
	}
	delete(s.prepared, r.Settings.Runtime)
	if r.Settings == s.app.Backend.RuntimeSettings() && r.Action != "check-update" && r.Action != "cancel-login" {
		s.reconnectRequired = true
	}
	var e error
	message := ""
	var managed *api.RuntimeStatus
	switch r.Action {
	case "install", "update", "check-update", "start", "apply-update":
		if r.Settings.Runtime == "caelis" {
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
		u, e = s.codex.Apply(ctx, path.Path, r.Action, r.APIKey, r.LoginID)
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
func (s *runtimeSetup) selectModel(ctx context.Context, r api.SetupRequest) (api.SetupState, error) {
	out, e := s.preparedState(ctx, r.Settings)
	if e != nil {
		return out, e
	}
	found := false
	var effort string
	for _, m := range out.Models {
		if m.Value == r.Model && !m.NoAuth {
			found = true
			if m.Parameters != nil {
				effort = m.Parameters.DefaultEffort
			}
		}
	}
	if !found {
		return out, errors.New(s.app.text("selectAvailableModel", nil))
	}
	delete(s.prepared, r.Settings.Runtime)
	if r.Settings.Runtime == "caelis" {
		if e = caelis.ApplySetup(ctx, r); e != nil {
			return out, e
		}
	}
	dir, _ := providerDirectory(s.app.root, r.Settings.Runtime)
	defaults := api.ExecutionSettings{ApprovalMode: "workspace-write"}
	if r.Settings.Runtime == "codex" {
		defaults.ApprovalMode = "auto"
	}
	preferences, e := backend.LoadExecutionSettings(filepath.Join(dir, "execution.json"), defaults)
	if e != nil {
		return out, e
	}
	preferences.Model = r.Model
	preferences.ServiceTier = ""
	preferences.Effort = ""
	if r.Settings.Runtime == "codex" {
		preferences.Effort = effort
	}
	if s.app.Backend.RuntimeSettings() == r.Settings && s.app.currentEngine().Snapshot().Connection == "ready" {
		e = s.app.Backend.SaveExecutionSettings(ctx, preferences)
	} else {
		e = localstate.Write(filepath.Join(dir, "execution.json"), preferences)
	}
	if e == nil {
		out.SelectedModel = r.Model
		// Native mutation confirmed; do not query the same catalog a second time.
		s.rememberPreparation(ctx, out)
	}
	return out, e
}
func (s *runtimeSetup) Activate(ctx context.Context, v api.RuntimeSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.app.guardConversationChange(); e != nil {
		return e
	}
	out, e := s.preparedState(ctx, v)
	if e != nil {
		return e
	}
	if out.State != "ready" {
		return errors.New(s.app.text("completeRuntimeConnectionFirst", nil))
	}

	if !s.reconnectRequired && s.canReuseRuntime(v) {
		s.pending, s.fresh = "", false
		return nil
	}
	if e = s.app.activateRuntime(ctx, v, !s.app.HasRuntimeChoice()); e != nil {
		delete(s.prepared, v.Runtime)
		return e
	}
	s.reconnectRequired = false
	s.rememberPreparation(ctx, out)
	s.activePreparation = s.prepared[v.Runtime]
	s.activeEngine = s.app.currentEngine()
	s.pending = ""
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
	for _, name := range []string{"runtime.json", "conversation.json"} {
		if _, e := os.Stat(filepath.Join(a.root, name)); !os.IsNotExist(e) {
			return true
		}
	}
	// Only pre-Notebook identities imply the historical Codex default. Creating
	// personal data offline must not silently choose a Runtime on next launch.
	var legacy struct {
		Version         int `json:"version"`
		PersonalVersion int `json:"personalVersion"`
	}
	b, e := os.ReadFile(filepath.Join(a.root, "bot.json"))
	return e == nil && json.Unmarshal(b, &legacy) == nil && legacy.Version == 1 && legacy.PersonalVersion == 0
}
func (a *Application) PrepareRestart() error { return a.Backend.PrepareRestart(a.guardRuntimeChange) }
