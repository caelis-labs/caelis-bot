// Package app composes product services without a desktop toolkit or OS APIs.
// Native entry points provide effects; adapters retain execution authority.
package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
)

// Host provides native effects. None of these callbacks select a backend or own
// a conversation. Closing a window must not call Application.Close.
type Host struct {
	ResolveFiles func([]string) ([]api.InputFile, error)
	ConsumeFiles func([]string)
	OpenURL      func(string) error
	RevealFile   func(string) error
	TrashFile    func(string) error
	Gesture      func(string) error
	Notify       func(id, title, body string, reminder bool)
	Observe      func(api.Snapshot)
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
	closeOnce       sync.Once
	closeErr        error
}

func New(root string, host Host) (*Application, error) {
	return newApplication(root, host, resolveProvider)
}

func newApplication(root string, host Host, resolve factoryResolver) (*Application, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("应用数据目录必须是完整路径")
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
		return nil, errors.New("后端工厂与连接标识不一致")
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
	engine, err := factory.Open(providerConfig{Settings: settings, Execution: execution,
		WorkDirectory: filepath.Join(directory, "Work"), ConversationFile: filepath.Join(directory, "conversation.json")})
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
	app := &Application{Backend: service, engine: engine, root: root, host: host}
	for _, err := range []error{service.ConfigurePresentation(filepath.Join(directory, "preview.json")), service.ConfigureDraft(filepath.Join(directory, "draft.json"))} {
		if err != nil && host.ReportError != nil {
			host.ReportError(err)
		}
	}
	service.ConfigureRuntime(settingsFile, settings)
	service.ConfigureExecution(executionFile, execution)
	app.configureRuntimeManagement()
	app.configureSetup()
	return app, nil
}

// The current product promises a resident secretary with owned work delegation.
// A chat-only adapter can be tested independently, but must not silently replace it.
func requireAssistant(engine api.Engine, id string) error {
	provider, ok := engine.(api.Provider)
	if !ok || provider.ProviderInfo().ID != id {
		return errors.New("后端身份与配置不一致")
	}
	if _, ok := engine.(api.SnapshotObserver); !ok {
		return errors.New("后端缺少状态观察能力")
	}
	if _, remote := engine.(api.ControlCompanion); remote {
		return nil
	}
	if _, ok := engine.(api.BotToolBinder); !ok {
		return errors.New("后端缺少受限 Bot 工具连接")
	}
	if _, ok := engine.(api.TaskProvider); !ok {
		return errors.New("后端缺少独立任务委派能力")
	}
	if _, ok := engine.(api.TaskReporter); !ok {
		return errors.New("后端缺少有限任务汇报能力")
	}
	return nil
}

// Start runs only after native surfaces are ready. It binds the private tools
// before connecting, then starts bounded observation and resident scheduling.
func (a *Application) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("应用已停止")
	}
	if a.started {
		return nil
	}
	residentPath := filepath.Join(a.root, "bot.json")
	if _, remote := a.engine.(api.ControlCompanion); remote {
		residentPath = filepath.Join(a.root, "providers", a.engine.(api.Provider).ProviderInfo().ID, "bot.json")
	}
	resident, err := bot.New(residentPath, a.host.Gesture)
	if err != nil {
		return err
	}
	var bridge *bot.Bridge
	remote, controlOwned := a.engine.(api.ControlCompanion)
	if controlOwned {
		err = remote.BindDesktop(api.DesktopEffects{Execute: resident.ExecuteDesktop, Notify: a.host.Notify})
	} else {
		bridge, err = bot.Serve(resident)
		if err == nil {
			var executable string
			executable, err = os.Executable()
			if err == nil {
				err = a.engine.(api.BotToolBinder).ConfigureBotTools(bridge.Config(executable))
			}
		}
	}
	if err != nil {
		if bridge != nil {
			bridge.Close()
		}
		resident.Close()
		return err
	}
	resident.SetReminderNotifier(func(id, label string) {
		if a.host.Notify == nil {
			return
		}
		if text := []rune(label); len(text) > 100 {
			label = string(text[:100]) + "…"
		}
		a.host.Notify(id, "到时间了", label, true)
	})
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel, a.companion, a.bridge, a.started = cancel, resident, bridge, true
	a.Backend.SetBotStatus(resident.Status)
	if !controlOwned {
		resident.Start(a.engine)
	}
	a.workers.Add(2)
	go func() { defer a.workers.Done(); _ = a.Backend.Connect(ctx) }()
	go func() {
		defer a.workers.Done()
		observer := backend.NotificationObserver{Notify: a.host.Notify, SkipResults: controlOwned}
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
		}
	}()
	return nil
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
	})
	return a.closeErr
}

func (a *Application) AttachmentStorage() (api.AttachmentStorage, error) {
	if source, ok := a.engine.(api.AttachmentProvider); ok {
		return source.AttachmentStorage()
	}
	return api.AttachmentStorage{}, errors.New("当前后端不支持附件存储管理")
}
func (a *Application) CleanAttachments(ctx context.Context) (api.AttachmentStorage, error) {
	if source, ok := a.engine.(api.AttachmentProvider); ok && a.host.TrashFile != nil {
		return source.TrashOldAttachments(ctx, a.host.TrashFile)
	}
	return api.AttachmentStorage{}, errors.New("当前后端不支持附件清理")
}
