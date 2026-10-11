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
	"github.com/caelis-labs/caelis-bot/internal/machines"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/telegram"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
	"github.com/caelis-labs/caelis-bot/internal/updates"
	"github.com/caelis-labs/caelis-bot/internal/weixin"
)

// Host provides native effects. None of these callbacks select a backend or own
// a conversation. Closing a window must not call Application.Close.
type Host struct {
	DesktopControl api.ApplicationTools // optional private native desktop driver
	CareSample     func() care.Sample
	CareSources    []care.Source
	// Locale is read when presenting host-generated UI, never during model execution.
	Locale              func() i18n.Locale
	Diagnostics         *diagnosticlog.Logger
	ResolveFiles        func([]string) ([]api.InputFile, error)
	ConsumeFiles        func([]string) error
	OpenURL             func(string) error
	RevealFile          func(string) error
	TrashFile           func(string) error
	Gesture             func(string) error
	Notify              func(id, title, body string, reminder bool)
	DismissNotification func(id string)
	Observe             func(api.Snapshot)
	ObserveTasks        func([]api.TaskPreview)
	ReportError         func(error)
}

type Application struct {
	localWork           *localWorkers
	machines            *machines.Service
	taskPreferences     *tasks.PreferencesStore
	setup               *runtimeSetup
	Backend             *backend.Service
	Telegram            *telegram.Bridge
	Weixin              *weixin.Bridge
	textControl         *textchannel.Store
	engine              api.Engine
	host                Host
	root                string
	mu                  sync.Mutex
	pluginAdmission     sync.Mutex
	pluginSyncMu        sync.Mutex
	pluginSyncRunning   bool
	pluginSyncPending   bool
	pluginSyncRevision  uint64
	pluginSyncError     bool
	updateMu            sync.Mutex
	interactionNoticeMu sync.Mutex
	started, closed     bool
	updatePrepared      bool
	cancel              context.CancelFunc
	startupCancel       context.CancelFunc
	dreamReady          bool
	careReady           bool
	workers             sync.WaitGroup
	companion           *bot.Runtime
	bridge              *bot.Bridge
	tasks               *tasks.Manager
	personal            *botmemory.Store
	notebook            *notebook.Vault
	skillPath           string
	initialization      *bot.Initializer
	plugins             *plugins.Manager
	pluginDetailMu      sync.Mutex
	pluginIndexMu       sync.Mutex
	pluginDetailCache   map[string]pluginDetailCacheEntry
	closeOnce           sync.Once
	closeErr            error
}

func New(root string, host Host) (*Application, error) {
	return newApplication(root, host, resolveProvider)
}

func newApplication(root string, host Host, resolve factoryResolver) (*Application, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New(i18n.Text(i18n.DefaultLocale, "host.appDataDirMustBeFullPath", nil))
	}
	if host.Diagnostics == nil {
		host.Diagnostics = diagnosticlog.NewAsync(filepath.Join(root, "Logs"))
	}
	settingsFile := filepath.Join(root, "runtime.json")
	settings, err := backend.LoadRuntimeSettings(settingsFile, "codex")
	runtimeReadErr := err
	if err != nil && host.ReportError != nil {
		host.ReportError(err)
	}
	factory, err := resolve(settings.Runtime)
	if err != nil {
		runtimeReadErr = errors.Join(runtimeReadErr, err)
		settings = api.RuntimeSettings{Runtime: "codex"}
		factory, err = resolve("codex")
		if err != nil {
			return nil, err
		}
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
		execution = factory.Defaults
		if factory.ID == "codex" {
			execution.ApprovalMode = "ask"
		}
		if host.ReportError != nil {
			host.ReportError(err)
		}
	}
	workExecutionFile := filepath.Join(directory, "work-execution.json")
	workExecution, err := backend.LoadWorkExecutionSettings(workExecutionFile)
	if err != nil {
		workExecution = api.WorkExecutionSettings{}
		if host.ReportError != nil {
			host.ReportError(err)
		}
	}
	engine, err := factory.Open(providerConfig{Diagnostics: host.Diagnostics, Settings: settings, Execution: execution, WorkExecution: workExecution,
		WorkDirectory: filepath.Join(directory, "Work"), WorkRoot: filepath.Join(root, "Tasks"), ConversationFile: filepath.Join(directory, "conversation.json")})
	if err != nil {
		runtimeReadErr = errors.Join(runtimeReadErr, err)
		settings = api.RuntimeSettings{Runtime: factory.ID}
		engine, err = factory.Open(providerConfig{Diagnostics: host.Diagnostics, Settings: settings, Execution: execution, WorkExecution: workExecution,
			WorkDirectory: filepath.Join(directory, "Work"), WorkRoot: filepath.Join(root, "Tasks"), ConversationFile: filepath.Join(directory, "conversation.json")})
		if err != nil {
			return nil, err
		}
	}
	if err = requireAssistant(engine, factory.ID); err != nil {
		if engine != nil {
			_ = backend.NewService(engine, nil, nil, nil, nil).Shutdown()
		}
		return nil, err
	}
	if runtimeReadErr == nil {
		if err = backend.SaveRuntimeProfile(root, settings); err != nil && host.ReportError != nil {
			host.ReportError(err)
		}
	}
	service := backend.NewService(engine, host.ResolveFiles, host.ConsumeFiles, host.OpenURL, host.RevealFile)
	service.ConfigureChat(filepath.Join(root, "chat.sqlite"))
	if err := backend.ConfigureScreenMedia(service, filepath.Join(root, "ScreenMedia")); err != nil && host.ReportError != nil {
		host.ReportError(err)
	}
	if err := backend.ConfigureMessageMedia(service, filepath.Join(root, "MessageMedia")); err != nil && host.ReportError != nil {
		host.ReportError(err)
	}
	initialization, err := bot.OpenInitializer(filepath.Join(root, "bot-initialization.json"))
	if err != nil {
		initialization = bot.UnavailableInitializer(filepath.Join(root, "bot-initialization.json"), err)
		if host.ReportError != nil {
			host.ReportError(err)
		}
	}
	app := &Application{Backend: service, engine: engine, root: root, host: host, initialization: initialization}
	service.SetGuardianNoticeAccepted(func(id string) bool { return app.workerInteractionNoticeAccepted("approval", id) })
	app.plugins, err = plugins.Open(filepath.Join(root, "Plugins"))
	if err != nil && host.ReportError != nil {
		host.ReportError(err)
	}
	service.SetBotPluginSource(app.PluginSnapshot)
	app.taskPreferences, err = tasks.OpenPreferences(filepath.Join(root, "task-preferences.json"))
	if err != nil {
		app.taskPreferences = tasks.UnavailablePreferences(filepath.Join(root, "task-preferences.json"))
		if host.ReportError != nil {
			host.ReportError(err)
		}
	}
	service.ConfigureInitialization(initialization)
	for _, err := range []error{service.ConfigurePresentation(filepath.Join(directory, "preview.json")), service.ConfigureDraft(filepath.Join(directory, "draft.json"))} {
		if err != nil && host.ReportError != nil {
			host.ReportError(err)
		}
	}
	service.ConfigureRuntime(settingsFile, settings)
	service.ConfigureRuntimeReadError(runtimeReadErr)
	service.ConfigureExecution(executionFile, execution)
	service.ConfigureWorkExecution(workExecutionFile, workExecution)
	app.configureRuntimeManagement()
	app.configureSetup()
	if err = app.configureWorkers(); err != nil && host.ReportError != nil {
		host.ReportError(err)
	}
	app.machines, err = machines.Open(filepath.Join(root, "Machines"), app.localWork, remoteArtifact)
	if err != nil && host.ReportError != nil {
		host.ReportError(err)
	}
	service.ConfigureMachines(app.machines)
	app.textControl, err = textchannel.Open(root, app.Backend.Decide)
	if err != nil {
		return nil, err // fail closed: a lost origin/decision ledger cannot broadcast or replay
	}
	showNotice := func(notice textchannel.Notice) {
		service.ObserveChat(api.Snapshot{Items: []api.Item{{ID: "control:" + notice.ID, Kind: "controlNotice", Text: notice.Text, Status: "completed", SeenAt: notice.SeenAt}}})
	}
	app.textControl.SetNoticeObserver(showNotice)
	app.textControl.SetAsyncUpdateObserver(func(item api.Item) {
		if !strings.HasPrefix(item.ID, "worker-question:") {
			service.ObserveChat(api.Snapshot{Items: []api.Item{item}})
		}
	})
	app.textControl.SetAsyncAnswerer(func(ctx context.Context, in textchannel.Inbound, modelInput string) (api.Receipt, error) {
		return backend.SubmitRemote(ctx, service, api.Submission{ID: in.ID, Text: in.Text, ModelInputOverride: modelInput}, nil)
	})
	app.textControl.SetAsyncWorkAnswerer(app.answerWorkerQuestion)
	service.SetCommandHandler(func(ctx context.Context, input api.Submission) (api.Receipt, bool, error) {
		if !textchannel.IsCommand(input.Text) {
			return api.Receipt{}, false, nil
		}
		feedback := app.textControl.Handle(ctx, textchannel.Inbound{Channel: "desktop", Conversation: "local", ID: input.ID, Text: input.Text}, service.Snapshot())
		return api.Receipt{ID: input.ID, Outcome: "accepted", Message: feedback}, true, nil
	})
	app.textControl.SetInboundObserver(func(in textchannel.Inbound) {
		if origin, ok := app.textControl.OriginOf(in.ID); ok {
			if origin.Channel != in.Channel {
				return
			}
		} else {
			if err := app.textControl.RecordOrigin(in.ID, textchannel.Origin{Channel: in.Channel, Conversation: in.Conversation}); err != nil {
				return
			}
		}
		body := in.Text
		if app.textControl.SecretPrompt(in) != "" {
			body = "已填写敏感字段"
		}
		service.ObserveChat(api.Snapshot{Items: []api.Item{{ID: "control-input:" + in.ID, RequestID: in.ID, Kind: "user", Text: body, Status: "accepted"}}})
	})
	recordInput := func(in textchannel.Inbound, secret bool) {
		if in.ID == "" || app.textControl.RecordOrigin(in.ID, textchannel.Origin{Channel: in.Channel, Conversation: in.Conversation}) != nil {
			return
		}
		body := in.Text
		if secret || app.textControl.SecretPrompt(in) != "" {
			body = "已填写敏感字段"
		}
		service.ObserveChat(api.Snapshot{Items: []api.Item{{ID: "channel-input:" + in.ID, RequestID: in.ID, Kind: "user", Text: body, Status: "received"}}})
	}
	// Import only the previous visible window into the local IM transcript.
	// Delivery ledgers still prevent any channel from replaying old notices.
	notices := app.textControl.Notices()
	if len(notices) > 32 {
		notices = notices[len(notices)-32:]
	}
	for _, notice := range notices {
		showNotice(notice)
	}
	if app.Telegram == nil {
		remote, remoteErr := telegram.Open(app.root, telegram.Host{
			Snapshot:    app.Backend.Snapshot,
			Diagnostics: app.host.Diagnostics.Write,
			Recovery:    app.Backend.RecoveryState,
			Recover:     app.Backend.RecoverIfCurrent,
			Submit: func(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
				return backend.SubmitRemote(ctx, app.Backend, in, files)
			},
			Interrupt: app.Backend.Interrupt, Decide: app.Backend.Decide,
			Artifact:    func(id string) (string, error) { return backend.ResolveRemoteArtifact(app.Backend, id) },
			ScreenImage: func(id string) ([]byte, error) { return backend.ScreenImageBytes(app.Backend, id) },
			Chinese:     func() bool { return app.locale() == i18n.Chinese },
			TextControl: app.textControl,
			RecordInput: recordInput,
		})
		if remoteErr != nil {
			if app.host.ReportError != nil {
				app.host.ReportError(remoteErr)
			}
		}
		if remote != nil {
			app.Telegram = remote
			backend.ObserveSubmissions(app.Backend, remote.Accepted)
		}
	}
	if app.Weixin == nil {
		remote, remoteErr := weixin.Open(app.root, weixin.Host{
			Snapshot: app.Backend.Snapshot,
			Recovery: app.Backend.RecoveryState,
			Submit: func(ctx context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
				return backend.SubmitRemote(ctx, app.Backend, in, files)
			},
			TextControl: app.textControl,
			RecordInput: recordInput,
		})
		if remoteErr != nil && app.host.ReportError != nil {
			app.host.ReportError(remoteErr)
		}
		app.Weixin = remote
	}
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
	if a.companion == nil {
		// Preserve a legacy choice before upgrading the offline identity.
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
		a.companion = resident
		resident.ConfigureDesktopControl(a.host.DesktopControl)
		resident.ConfigureInitialization(a.initialization)
	}
	resident := a.companion
	report := func(err error) {
		if err != nil && a.host.ReportError != nil {
			a.host.ReportError(err)
		}
	}
	if !a.careReady {
		if err := resident.ConfigureCare(a.host.CareSample, a.host.CareSources...); err != nil {
			report(err)
		} else {
			a.careReady = true
		}
	}
	if a.personal == nil {
		personal, err := botmemory.Open(context.Background(), filepath.Join(a.root, "personal"), resident.State().ID)
		if err == nil {
			err = resident.ConfigurePersonal(personal)
		}
		if err != nil {
			if personal != nil {
				personal.Close()
			}
			report(err)
		} else {
			a.personal = personal
		}
	}
	if a.notebook == nil {
		vault, err := notebook.OpenVault(filepath.Join(a.root, "Notebook"))
		if err == nil {
			marker := filepath.Join(a.root, "notebook-migration.json")
			if _, e := os.Stat(marker); errors.Is(e, os.ErrNotExist) && a.personal != nil {
				profile, e := a.personal.LegacyProfile(context.Background())
				err = e
				if err == nil {
					err = vault.Migrate(filepath.Join(a.root, "personal", "notebook"), marker, profile, time.Now())
				}
			} else if e == nil {
				err = vault.Migrate("", marker, "", time.Now())
			} else if e != nil && !errors.Is(e, os.ErrNotExist) {
				err = e
			}
			if err == nil {
				err = vault.Refresh(context.Background(), time.Now())
			}
		}
		if err != nil {
			if vault != nil {
				vault.Close()
			}
			report(err)
		} else {
			a.notebook = vault
		}
	}
	if a.skillPath == "" {
		path, err := botskills.Install(a.root)
		if err != nil {
			report(err)
		} else {
			a.skillPath = path
		}
	}
	if a.notebook != nil && a.skillPath != "" && !a.dreamReady {
		if err := resident.ConfigureDream(a.notebook, a.skillPath); err != nil {
			report(err)
		} else {
			a.dreamReady = true
		}
	}

	a.companion = resident
	return nil
}

// Start runs only after native surfaces are ready. It binds the private tools
// before connecting, then starts bounded observation and resident scheduling.
func (a *Application) Start() error {
	a.pluginAdmission.Lock()
	defer a.pluginAdmission.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New(a.text("host.appStopped"))
	}
	if a.started {
		return a.repairFeaturesLocked()
	}
	manager, err := tasks.Open(filepath.Join(a.root, "tasks.json"), filepath.Join(a.root, "Tasks"), a.engine.(api.Provider).ProviderInfo().ID, a.machines, a.engine.(api.ReportSubmitter), a.engine.Snapshot)
	if err != nil && a.host.ReportError != nil {
		a.host.ReportError(err)
	}
	if importer, ok := a.engine.(interface{ ImportHostReportIDs([]string) error }); ok && manager != nil {
		if err := importer.ImportHostReportIDs(manager.HostReportIDs()); err != nil && a.host.ReportError != nil {
			a.host.ReportError(err)
		}
	}
	manager.SetLocale(a.locale)
	if manager != nil {
		manager.ConfigureLimit(func() int { return a.taskPreferences.Snapshot().MaxRunning })
		manager.ObserveWatchlist(a.host.ObserveTasks)
		manager.ObserveStarts(func(task api.Task) {
			status := task.Status
			text := "Worker 已开始：" + task.Title
			if task.Outcome == "unknown" || status == "unknown" {
				text = "Worker 状态待确认：" + task.Title
			}
			a.Backend.ObserveChat(api.Snapshot{Items: []api.Item{{
				ID: "task-start:" + task.ID, Kind: "assistant", Text: text,
				Status: "completed", Task: &api.TaskPresentation{ID: task.ID, Title: task.Title, Status: status},
			}}})
		})
	}
	if err = a.preparePersonalLocked(); err != nil {
		return err
	}
	resident := a.companion
	if manager != nil {
		err = resident.ConfigureTasks(manager, manager)
	}
	resident.ConfigureWorkerInteractions(a)
	if err != nil && manager != nil {
		return err
	}
	configuredPluginRevision := uint64(0)
	bridge, err := bot.Serve(resident)
	if err == nil {
		var executable string
		executable, err = os.Executable()
		if err == nil {
			config := bridge.Config(executable)
			if a.skillPath != "" {
				config.SkillInstructions = botskills.Instructions(a.skillPath)
				config.BuiltinSkillRoots = []string{filepath.Dir(a.skillPath)}
			}
			if a.plugins != nil {
				config.Plugins = a.plugins.Selection()
			}
			configuredPluginRevision = config.Plugins.Revision
			config.NotebookDirectory = filepath.Join(a.root, "Notebook")
			config.RuntimeVersion = updates.Version
			config.PrepareContext = func(ctx context.Context) (api.ContextSeed, error) {
				a.mu.Lock()
				vault := a.notebook
				a.mu.Unlock()
				var seed api.ContextSeed
				if vault == nil {
					seed = api.ContextSeed{}
				} else {
					var err error
					seed, err = vault.PrepareContext(ctx)
					if err != nil {
						if a.host.ReportError != nil {
							a.host.ReportError(err)
						}
						seed = api.ContextSeed{}
					}
				}
				handoff := resident.PrepareHandoffContext()
				seed.Text += handoff.Text
				seed.HandoffDigest = handoff.HandoffDigest
				return seed, nil
			}
			config.ConsumeContext = func(seed api.ContextSeed) error {
				return resident.ConsumeHandoffContext(seed)
			}
			config.PrepareTurn = func(ctx context.Context) error {
				if err := a.syncPluginIndex(ctx); err != nil && a.host.ReportError != nil {
					a.host.ReportError(err)
				}
				a.mu.Lock()
				vault := a.notebook
				a.mu.Unlock()
				if vault != nil {
					if err := vault.Refresh(ctx, time.Now()); err != nil && a.host.ReportError != nil {
						a.host.ReportError(err)
					}
				}
				resident.BeginDesktopTurn()
				return nil
			}
			config.FinishTurn = func() {
				resident.StopDesktopTurn()
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := resident.RenewPendingToolHandoff(ctx); err != nil && a.host.ReportError != nil {
						a.host.ReportError(err)
					}
				}()
				// Core can assemble a fresh MCP manager during this turn. Refresh
				// the host index after readiness without delaying the Runtime's
				// completion notification or replaying any tool operation.
				if a.plugins != nil && len(a.plugins.Selection().Servers) != 0 {
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if err := a.syncPluginIndex(ctx); err != nil && a.host.ReportError != nil {
							a.host.ReportError(err)
						}
					}()
				}
				a.mu.Lock()
				vault := a.notebook
				a.mu.Unlock()
				if vault != nil {
					if e := vault.Refresh(context.Background(), time.Now()); e != nil && a.host.ReportError != nil {
						a.host.ReportError(e)
					}
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
	if a.plugins != nil {
		a.pluginSyncMu.Lock()
		// This is the exact selection passed to ConfigureBotTools above.
		// A management write during Start must remain pending for reconcile.
		a.pluginSyncRevision = configuredPluginRevision
		a.pluginSyncMu.Unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel, a.companion, a.bridge, a.tasks, a.started = cancel, resident, bridge, manager, true
	a.Backend.SetBotStatus(resident.Status)
	a.Backend.SetInterruptObserver(resident.StopDesktopTurn)
	resident.Start(a.engine)
	a.Backend.SetUserSubmitter(resident.SubmitUser)
	if a.Telegram != nil {
		a.Telegram.Start()
	}
	if a.Weixin != nil {
		a.Weixin.Start()
	}
	a.workers.Add(6)
	go func() { defer a.workers.Done(); a.localWork.Observe(ctx) }()
	go func() {
		defer a.workers.Done()
		a.observeWorkerQuestions(ctx)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.observeWorkerQuestions(ctx)
			}
		}
	}()
	go func() { defer a.workers.Done(); a.machines.Observe(ctx) }()
	go func() {
		defer a.workers.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.mu.Lock()
				manager := a.tasks
				a.mu.Unlock()
				if manager != nil {
					if err := manager.RefreshWatchlist(); err != nil {
						if a.host.ReportError != nil {
							a.host.ReportError(err)
						}
					}
					recovery := a.Backend.RecoveryState()
					if a.host.Notify != nil && !recovery.Automatic && !recovery.InProgress {
						ids, err := manager.ClaimUnknownNotices()
						if err != nil {
							if a.host.ReportError != nil {
								a.host.ReportError(err)
							}
							continue
						}
						for _, id := range ids {
							a.host.Notify("task-unknown-"+id, a.text("host.taskUnknownTitle", map[string]any{"id": id}), a.text("host.taskUnknownBody"), true)
						}
					}
				}
			}
		}
	}()
	go func() {
		defer a.workers.Done()
		if err := a.Backend.Connect(ctx); err == nil {
			a.queuePluginReconcile()
		}
	}()
	go func() {
		defer a.workers.Done()
		observer := backend.NotificationObserver{Notify: a.host.Notify, Dismiss: a.host.DismissNotification, Locale: a.host.Locale}
		var revision uint64
		var priorConnection string
		var priorPluginRecoveryPending bool
		for {
			snapshot, err := a.engine.(api.SnapshotObserver).WaitSnapshot(ctx, revision)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				if a.host.ReportError != nil {
					a.host.ReportError(err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
				continue
			}
			revision = snapshot.Revision
			a.observePluginReadiness(snapshot.Connection, &priorConnection, &priorPluginRecoveryPending)
			a.observeWorkerApprovalNotice(ctx, snapshot)
			snapshot = a.userVisibleApprovals(snapshot)
			for i, item := range snapshot.Items {
				if len(item.AsyncQuestions) == 0 {
					continue
				}
				card, cardErr := a.textControl.AsyncCard(item, snapshot.RuntimeOwner)
				if cardErr != nil {
					if a.host.ReportError != nil {
						a.host.ReportError(cardErr)
					}
					continue
				}
				snapshot.Items[i] = card
			}
			if a.Weixin != nil {
				a.Weixin.ObserveTurn(snapshot)
			}
			a.Backend.ObserveChat(snapshot)
			observer.Observe(snapshot)
			if a.host.Observe != nil {
				a.host.Observe(snapshot)
			}
			if a.host.ObserveTasks != nil {
				a.mu.Lock()
				manager := a.tasks
				a.mu.Unlock()
				if manager != nil {
					a.host.ObserveTasks(manager.TaskPreviews())
				}
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
	defer a.host.Diagnostics.Close()
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		if a.startupCancel != nil {
			a.startupCancel()
		}
		if a.cancel != nil {
			a.cancel()
		}
		resident, bridge := a.companion, a.bridge
		updating := a.updatePrepared
		if resident != nil {
			resident.Stop()
		}
		a.mu.Unlock()
		if a.Telegram != nil {
			a.Telegram.Close()
		}
		if a.Weixin != nil {
			a.Weixin.Close()
		}
		if a.setup != nil {
			a.setup.mu.Lock()
			a.setup.codex.Close()
			a.setup.connections.Close()
			a.setup.mu.Unlock()
		}
		if updating {
			a.closeErr = a.Backend.ShutdownForUpdate()
		} else {
			a.closeErr = a.Backend.Shutdown()
		}
		if resident != nil {
			resident.Close()
		}
		if bridge != nil {
			bridge.Close()
		}
		a.workers.Wait()
		if a.localWork != nil {
			if updating {
				a.closeErr = errors.Join(a.closeErr, a.localWork.CloseForUpdate())
			} else {
				a.closeErr = errors.Join(a.closeErr, a.localWork.Close())
			}
		}
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
	media, err := backend.ScreenMediaStorage(a.Backend, false, nil)
	if err != nil {
		return media, err
	}
	if source, ok := a.engine.(api.AttachmentProvider); ok {
		info, err := source.AttachmentStorage()
		if err != nil {
			return info, err
		}
		info.Files += media.Files
		info.Bytes += media.Bytes
		info.EligibleFiles += media.EligibleFiles
		info.EligibleBytes += media.EligibleBytes
		info.CanClean = (info.CanClean || media.CanClean) && info.Notice == "" && a.guardRuntimeChange() == nil
		return info, nil
	}
	media.CanClean = media.CanClean && a.guardRuntimeChange() == nil
	return media, nil
}
func (a *Application) CleanAttachments(ctx context.Context) (api.AttachmentStorage, error) {
	if err := a.guardRuntimeChange(); err != nil {
		return api.AttachmentStorage{}, err
	}
	if source, ok := a.engine.(api.AttachmentProvider); ok && a.host.TrashFile != nil {
		if _, err := source.TrashOldAttachments(ctx, a.host.TrashFile); err != nil {
			return api.AttachmentStorage{}, err
		}
	}
	if a.host.TrashFile == nil {
		return api.AttachmentStorage{}, errors.New(a.text("backendNoAttachmentClean", nil))
	}
	if _, err := backend.ScreenMediaStorage(a.Backend, true, a.host.TrashFile); err != nil {
		return api.AttachmentStorage{}, err
	}
	return a.AttachmentStorage()
}
