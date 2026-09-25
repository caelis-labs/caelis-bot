// Package app composes product services without a desktop toolkit or OS APIs.
// Native entry points provide effects; adapters retain execution authority.
package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botmemory"
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/care"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

// Host provides native effects. None of these callbacks select a backend or own
// a conversation. Closing a window must not call Application.Close.
type Host struct {
	CareSample  func() care.Sample
	CareSources []care.Source
	// Locale is read when presenting host-generated UI, never during model execution.
	Locale       func() i18n.Locale
	Diagnostics  *diagnosticlog.Logger
	ResolveFiles func([]string) ([]api.InputFile, error)
	ConsumeFiles func([]string)
	OpenURL      func(string) error
	RevealFile   func(string) error
	TrashFile    func(string) error
	Gesture      func(string) error
	Notify       func(id, title, body string, reminder bool)
	Observe      func(api.Snapshot)
	ObserveTasks func([]api.TaskPreview)
	ReportError  func(error)
}

type Application struct {
	setup           *runtimeSetup
	Backend         *backend.Service
	engine          api.Engine
	host            Host
	root            string
	mu              sync.Mutex
	started, closed bool
	cancel          context.CancelFunc
	workers         sync.WaitGroup
	companion       *bot.Runtime
	bridge          *bot.Bridge
	tasks           *tasks.Manager
	personal        *botmemory.Store
	notebook        *notebook.Vault
	skillPath       string
	initialization  *bot.Initializer
	closeOnce       sync.Once
	closeErr        error
}

func New(root string, host Host) (*Application, error) {
	return newApplication(root, host, resolveProvider)
}

func newApplication(root string, host Host, resolve factoryResolver) (*Application, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New(i18n.Text(i18n.DefaultLocale, "host.appDataDirMustBeFullPath", nil))
	}
	if host.Diagnostics == nil {
		host.Diagnostics = diagnosticlog.New(filepath.Join(root, "Logs"))
	}
	settingsFile := filepath.Join(root, "runtime.json")
	settings, err := backend.LoadRuntimeSettings(settingsFile, "codex")
	if err != nil {
		return nil, err
	}
	factory, err := resolve(settings.Runtime)
	if err != nil {
		return nil, err
	}
	if factory.ID != settings.Runtime || factory.Open == nil {
		return nil, errors.New(i18n.Text(i18n.DefaultLocale, "host.backendFactoryMismatch", nil))
	}
	directory, err := providerDirectory(root, factory.ID)
	if err != nil {
		return nil, err
	}
	executionFile := filepath.Join(directory, "execution.json")
	execution, err := backend.LoadExecutionSettings(executionFile, factory.Defaults)
	if err != nil {
		return nil, err
	}
	workExecutionFile := filepath.Join(directory, "work-execution.json")
	workExecution, err := backend.LoadWorkExecutionSettings(workExecutionFile)
	if err != nil {
		return nil, err
	}
	engine, err := factory.Open(providerConfig{Diagnostics: host.Diagnostics, Settings: settings, Execution: execution, WorkExecution: workExecution,
		WorkDirectory: filepath.Join(directory, "Work"), WorkRoot: filepath.Join(root, "Tasks"), ConversationFile: filepath.Join(directory, "conversation.json")})
	if err != nil {
		return nil, err
	}
	if err = requireAssistant(engine, factory.ID); err != nil {
		if engine != nil {
			_ = backend.NewService(engine, nil, nil, nil, nil).Shutdown()
		}
		return nil, err
	}
	service := backend.NewService(engine, host.ResolveFiles, host.ConsumeFiles, host.OpenURL, host.RevealFile)
	initialization, err := bot.OpenInitializer(filepath.Join(root, "bot-initialization.json"))
	if err != nil {
		_ = service.Shutdown()
		return nil, err
	}
	app := &Application{Backend: service, engine: engine, root: root, host: host, initialization: initialization}
	service.ConfigureInitialization(initialization)
	for _, err := range []error{service.ConfigurePresentation(filepath.Join(directory, "preview.json")), service.ConfigureDraft(filepath.Join(directory, "draft.json"))} {
		if err != nil && host.ReportError != nil {
			host.ReportError(err)
		}
	}
	service.ConfigureRuntime(settingsFile, settings)
	service.ConfigureExecution(executionFile, execution)
	service.ConfigureWorkExecution(workExecutionFile, workExecution)
	app.configureRuntimeManagement()
	app.configureSetup()
	return app, nil
}

// The current product promises a resident secretary with owned work delegation.
// A chat-only adapter can be tested independently, but must not silently replace it.
func requireAssistant(engine api.Engine, id string, loc ...i18n.Locale) error {
	l := i18n.DefaultLocale
	if len(loc) > 0 && loc[0] != "" {
		l = loc[0]
	}
	provider, ok := engine.(api.Provider)
	if !ok || provider.ProviderInfo().ID != id {
		return errors.New(i18n.Text(l, "host.backendIdentityMismatch", nil))
	}
	if _, ok := engine.(api.SnapshotObserver); !ok {
		return errors.New(i18n.Text(l, "host.backendMissingObservation", nil))
	}
	if _, ok := engine.(api.BotToolBinder); !ok {
		return errors.New(i18n.Text(l, "host.backendMissingBotTools", nil))
	}
	if _, ok := engine.(api.WorkRuntime); !ok {
		return errors.New(i18n.Text(l, "host.backendMissingDelegation", nil))
	}
	if _, ok := engine.(api.ReportSubmitter); !ok {
		return errors.New(i18n.Text(l, "host.backendMissingReporting", nil))
	}
	return nil
}

// PreparePersonal makes local data available even before selecting/logging into
// a Runtime. It starts no model, scheduler, tool transport or execution session.
func (a *Application) PreparePersonal() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.preparePersonalLocked()
}
func (a *Application) preparePersonalLocked() error {
	if a.closed {
		return errors.New(a.text("host.appStopped"))
	}
	if a.personal != nil {
		return nil
	}
	// Preserve an earlier runtime choice represented solely by legacy bot.json
	// before introducing an offline-capable product identity.
	if a.HasRuntimeChoice() {
		if _, e := os.Stat(filepath.Join(a.root, "runtime.json")); errors.Is(e, os.ErrNotExist) {
			if e = localstate.Write(filepath.Join(a.root, "runtime.json"), a.Backend.RuntimeSettings()); e != nil {
				return e
			}
		}
	}
	resident, err := bot.NewForRuntime(filepath.Join(a.root, "bot.json"), a.engine.(api.Provider).ProviderInfo().ID, a.host.Gesture)
	if err != nil {
		return err
	}
	if err = resident.ConfigureCare(a.host.CareSample, a.host.CareSources...); err != nil {
		resident.Close()
		return err
	}
	personal, err := botmemory.Open(context.Background(), filepath.Join(a.root, "personal"), resident.State().ID)
	if err != nil {
		resident.Close()
		return err
	}
	if err = resident.ConfigurePersonal(personal); err != nil {
		personal.Close()
		resident.Close()
		return err
	}
	vault, err := notebook.OpenVault(filepath.Join(a.root, "Notebook"))
	if err != nil {
		personal.Close()
		resident.Close()
		return err
	}
	fail := func(err error) error { vault.Close(); personal.Close(); resident.Close(); return err }
	marker := filepath.Join(a.root, "notebook-migration.json")
	if _, err = os.Stat(marker); errors.Is(err, os.ErrNotExist) {
		var profile string
		profile, err = personal.LegacyProfile(context.Background())
		if err != nil {
			return fail(err)
		}
		if err = vault.Migrate(filepath.Join(a.root, "personal", "notebook"), marker, profile, time.Now()); err != nil {
			return fail(err)
		}
	} else if err != nil {
		return fail(err)
	} else if err = vault.Migrate("", marker, "", time.Now()); err != nil {
		return fail(err)
	}
	if err = vault.Refresh(context.Background(), time.Now()); err != nil {
		return fail(err)
	}
	skillPath, err := botskills.Install(a.root)
	if err != nil {
		return fail(err)
	}
	resident.ConfigureInitialization(a.initialization)
	a.companion, a.personal, a.notebook, a.skillPath = resident, personal, vault, skillPath
	return nil
}

// Start runs only after native surfaces are ready. It binds the private tools
// before connecting, then starts bounded observation and resident scheduling.
func (a *Application) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New(a.text("host.appStopped"))
	}
	if a.started {
		return nil
	}
	manager, err := tasks.Open(filepath.Join(a.root, "tasks.json"), filepath.Join(a.root, "Tasks"), a.engine.(api.Provider).ProviderInfo().ID, a.engine.(api.WorkRuntime), a.engine.(api.ReportSubmitter), a.engine.Snapshot)
	if err != nil {
		return err
	}
	manager.SetLocale(a.locale)
	if err = a.preparePersonalLocked(); err != nil {
		return err
	}
	resident := a.companion
	if err = resident.ConfigureTasks(manager, manager); err != nil {
		return err
	}
	bridge, err := bot.Serve(resident)
	if err == nil {
		var executable string
		executable, err = os.Executable()
		if err == nil {
			config := bridge.Config(executable)
			config.Instructions += botskills.Instructions(a.skillPath)
			config.NotebookDirectory = a.notebook.Path()
			config.PrepareTurn = func(ctx context.Context) error { return a.notebook.Refresh(ctx, time.Now()) }
			config.FinishTurn = func() {
				if e := a.notebook.Refresh(context.Background(), time.Now()); e != nil && a.host.ReportError != nil {
					a.host.ReportError(e)
				}
			}
			err = a.engine.(api.BotToolBinder).ConfigureBotTools(config)
		}
	}
	if err != nil {
		if bridge != nil {
			bridge.Close()
		}
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel, a.companion, a.bridge, a.tasks, a.started = cancel, resident, bridge, manager, true
	a.Backend.SetBotStatus(resident.Status)
	resident.Start(a.engine)
	a.workers.Add(2)
	go func() { defer a.workers.Done(); _ = a.Backend.Connect(ctx) }()
	go func() {
		defer a.workers.Done()
		observer := backend.NotificationObserver{Notify: a.host.Notify, Locale: a.host.Locale}
		var revision uint64
		for {
			snapshot, err := a.engine.(api.SnapshotObserver).WaitSnapshot(ctx, revision)
			if err != nil || ctx.Err() != nil {
				return
			}
			revision = snapshot.Revision
			observer.Observe(snapshot)
			if a.host.Observe != nil {
				a.host.Observe(snapshot)
			}
			if a.host.ObserveTasks != nil {
				a.host.ObserveTasks(manager.TaskPreviews())
			}
		}
	}()
	return nil
}

func (a *Application) locale() i18n.Locale {
	if a != nil && a.host.Locale != nil {
		return a.host.Locale()
	}
	return i18n.English
}
func (a *Application) text(key string, args ...map[string]any) string {
	if !strings.HasPrefix(key, "host.") {
		key = "host." + key
	}
	var m map[string]any
	if len(args) > 0 {
		m = args[0]
	}
	return i18n.Text(a.locale(), key, m)
}

func (a *Application) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	a.mu.Lock()
	m, stopped := a.tasks, a.closed
	a.mu.Unlock()
	if m == nil || stopped {
		return api.TerminalTarget{}, errors.New(a.text("taskNotConnected", nil))
	}
	return m.WorkTerminal(ctx, id)
}

// Close is the explicit application-exit boundary. Cancellation stops wakeups
// before the adapter cleans only owned work; shared servers remain alive.
func (a *Application) Close() error {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		if a.cancel != nil {
			a.cancel()
		}
		resident, bridge := a.companion, a.bridge
		if resident != nil {
			resident.Stop()
		}
		a.mu.Unlock()
		if a.setup != nil {
			a.setup.mu.Lock()
			a.setup.codex.Close()
			a.setup.connections.Close()
			a.setup.mu.Unlock()
		}
		a.closeErr = a.Backend.Shutdown()
		if resident != nil {
			resident.Close()
		}
		if bridge != nil {
			bridge.Close()
		}
		a.workers.Wait()
		if a.notebook != nil {
			a.closeErr = errors.Join(a.closeErr, a.notebook.Close())
		}
		if a.personal != nil {
			a.closeErr = errors.Join(a.closeErr, a.personal.Close())
		}
	})
	return a.closeErr
}

func (a *Application) AttachmentStorage() (api.AttachmentStorage, error) {
	if source, ok := a.engine.(api.AttachmentProvider); ok {
		return source.AttachmentStorage()
	}
	return api.AttachmentStorage{}, errors.New(a.text("backendNoAttachmentStorage", nil))
}
func (a *Application) CleanAttachments(ctx context.Context) (api.AttachmentStorage, error) {
	if source, ok := a.engine.(api.AttachmentProvider); ok && a.host.TrashFile != nil {
		return source.TrashOldAttachments(ctx, a.host.TrashFile)
	}
	return api.AttachmentStorage{}, errors.New(a.text("backendNoAttachmentClean", nil))
}
