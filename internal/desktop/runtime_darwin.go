//go:build darwin && cgo

package desktop

import (
	"context"
	_ "embed"
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/updates"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed assets/app-icon.png
var appIcon []byte

func Run(assets fs.FS) error {
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	s := newService(fileStore{filepath.Join(config, "Caelis Bot", "placement.json")})
	root := filepath.Join(config, "Caelis Bot")
	s.configureShortcut(filepath.Join(root, "shortcut.json"))
	logError(s.configureSelection(filepath.Join(root, "draft-files.json")))
	runtimeFile := filepath.Join(root, "runtime.json")
	runtimeSettings, err := backend.LoadRuntimeSettings(runtimeFile)
	if err != nil {
		return err
	}
	executionFile := filepath.Join(root, "execution.json")
	executionSettings, err := backend.LoadExecutionSettings(executionFile)
	if err != nil {
		return err
	}
	cliPath := runtimeSettings.CLIPath
	if override := os.Getenv("CODEX_BIN"); override != "" {
		cliPath = override
	}
	var companion atomic.Pointer[bot.Runtime]
	notificationContext, stopNotifications := context.WithCancel(context.Background())
	var bridge *bot.Bridge
	defer func() {
		if bridge != nil {
			bridge.Close()
		}
	}()
	stopCompanion := func() {
		stopNotifications()
		if r := companion.Load(); r != nil {
			r.Stop()
		}
	}
	engine := codex.NewSession(codex.SessionOptions{Execution: executionSettings, Binary: cliPath, Socket: os.Getenv("CAELIS_CODEX_SOCKET"), Directory: filepath.Join(root, "Work"), StateFile: filepath.Join(root, "conversation.json")})
	back := backend.NewService(engine, s.resolveDraftFiles, s.consumeDraftFiles,
		func(url string) error { return exec.Command("/usr/bin/open", url).Run() },
		func(path string) error { return exec.Command("/usr/bin/open", "-R", path).Run() })
	logError(back.ConfigurePresentation(filepath.Join(root, "preview.json")))
	logError(back.ConfigureDraft(filepath.Join(root, "draft.json")))
	back.ConfigureRuntime(runtimeFile, runtimeSettings, engine.ChangeCLI)
	back.ConfigureExecution(executionFile, executionSettings)
	s.storage = engine.AttachmentStorage
	s.cleanStorage = func(ctx context.Context) (api.AttachmentStorage, error) {
		return engine.TrashOldAttachments(ctx, trashNativePath)
	}
	s.diagnosticReport = back.DiagnosticReport
	s.activate = func() {
		v := back.ComposerSnapshot()
		for _, a := range v.Approvals {
			if a.Status != "resolved" {
				s.OpenApproval()
				return
			}
		}
		if v.CanSend {
			s.CloseHistory()
			s.TogglePanel()
		} else {
			s.OpenHistory()
		}
	}
	var app *application.App
	var quitting, finished atomic.Bool
	quit := func() {
		if quitting.CompareAndSwap(false, true) {
			go func() {
				stopCompanion()
				logError(back.Shutdown())
				if r := companion.Load(); r != nil {
					r.Close()
				}
				finished.Store(true)
				app.Quit()
			}()
		}
	}
	app = application.New(application.Options{
		Name: "Caelis Bot", Description: "A quiet desktop companion",
		Icon:                        appIcon,
		Assets:                      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Services:                    []application.Service{application.NewService(s), application.NewService(back)},
		Mac:                         application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory, ApplicationShouldTerminateAfterLastWindowClosed: false},
		DisableDefaultSignalHandler: true,
		ShouldQuit: func() bool {
			if finished.Load() {
				return true
			}
			quit()
			return false
		},
		OnShutdown: func() {
			stopCompanion()
			logError(back.Shutdown())
			if r := companion.Load(); r != nil {
				r.Close()
			}
			s.shutdown()
		},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "dev.caelis.bot", OnSecondInstanceLaunch: func(application.SecondInstanceData) { _ = s.SetVisible(true) }},
	})
	signals := make(chan os.Signal, 1)
	s.copyText = app.Clipboard.SetText
	s.openReleasePage = func() error { return exec.Command("/usr/bin/open", updates.ReleasePage).Run() }
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() { <-signals; quit() }()
	pet := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "pet", Title: "Caelis Bot — 桌宠", Width: 180, Height: 240, Frameless: true, DisableResize: true, Hidden: true,
		IgnoreMouseEvents: true, URL: "/?surface=pet", BackgroundType: application.BackgroundTypeTransparent,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, DisableShadow: true, CornerType: application.MacWindowCornerTypeSquare,
			WindowLevel: application.MacWindowLevelFloating, CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces | application.MacWindowCollectionBehaviorStationary | application.MacWindowCollectionBehaviorFullScreenAuxiliary | application.MacWindowCollectionBehaviorIgnoresCycle},
	})
	panel := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "conversation", Title: "Caelis Bot", Width: 420, Height: 64, Frameless: true, DisableResize: true, Hidden: true,
		URL: "/?surface=panel", BackgroundType: application.BackgroundTypeTransparent, EnableFileDrop: true,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, CornerType: application.MacWindowCornerTypeSquare, WindowLevel: application.MacWindowLevelFloating, CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace | application.MacWindowCollectionBehaviorFullScreenAuxiliary},
	})
	history := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "history", Title: "Caelis Bot", Width: 640, Height: 700, MinWidth: 420, MinHeight: 360,
		Hidden: true, URL: "/?surface=history", EnableFileDrop: true, BackgroundColour: application.NewRGB(247, 247, 247),
		Mac: application.MacWindow{TitleBar: application.MacTitleBar{AppearsTransparent: true}},
	})
	bubble := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "bubble", Title: "Caelis Bot — 消息", Width: 360, Height: 68, Frameless: true, DisableResize: true, Hidden: true,
		URL: "/?surface=bubble", BackgroundType: application.BackgroundTypeTransparent,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, DisableShadow: true},
	})
	prop := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "prop", Title: "Caelis Bot — 纸飞机", Width: 520, Height: 360, Frameless: true, DisableResize: true, Hidden: true,
		URL: "/?surface=prop", BackgroundType: application.BackgroundTypeTransparent,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, DisableShadow: true},
	})
	settings := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "settings", Title: "Caelis Bot — 设置", Width: 960, Height: 680, MinWidth: 760, MinHeight: 540,
		Hidden: true, URL: "/?surface=settings", BackgroundType: application.BackgroundTypeTransparent,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, TitleBar: application.MacTitleBar{AppearsTransparent: true}},
	})
	for _, window := range []*application.WebviewWindow{panel, bubble, settings} {
		window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) { syncMacMaterials() })
	}
	s.openSettings = func() {
		s.ClosePanel()
		s.CollapseBubble()
		settings.Show()
		settings.Focus()
		settings.ExecJS("window.dispatchEvent(new Event('settings-open'))")
	}
	s.closeSettings = func() { settings.Hide() }
	s.saveDiagnosticPath = func() (string, error) {
		return app.Dialog.SaveFile().AttachToWindow(settings).SetFilename("Caelis-Bot-diagnostics.json").
			SetMessage("仅包含系统、连接和状态计数；不包含聊天内容、文件路径或凭据。").
			AddFilter("JSON", "*.json").CanCreateDirectories(true).PromptForSingleSelection()
	}
	s.pickRuntimeCLI = func() (string, error) {
		return app.Dialog.OpenFile().AttachToWindow(settings).CanChooseFiles(true).CanChooseDirectories(false).SetTitle("选择 Codex CLI").SetButtonText("选择").PromptForSingleSelection()
	}
	settings.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); settings.Hide() })
	var historyOpen atomic.Bool
	s.openHistory = func() {
		s.ClosePanel()
		s.CollapseBubble()
		historyOpen.Store(true)
		history.Show()
		history.Focus()
		history.ExecJS("window.dispatchEvent(new Event('history-open'))")
	}
	s.closeHistory = func() {
		if !historyOpen.Swap(false) {
			return
		}
		history.ExecJS("window.dispatchEvent(new Event('history-close'))")
		history.Hide()
	}
	s.historyVisible = historyOpen.Load
	history.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); s.closeHistory() })
	for _, window := range []*application.WebviewWindow{panel, history} {
		window.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
			s.mu.Lock()
			_, err := s.stageFiles(e.Context().DroppedFiles())
			s.mu.Unlock()
			message := ""
			if err != nil {
				message = err.Error()
			}
			encoded, _ := json.Marshal(message)
			window.ExecJS("window.dispatchEvent(new CustomEvent('files-changed',{detail:" + string(encoded) + "}))")
		})
	}
	panel.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); s.ClosePanel() })
	s.pickFiles = func() ([]string, error) {
		return app.Dialog.OpenFile().AttachToWindow(func() *application.WebviewWindow {
			if historyOpen.Load() {
				return history
			}
			return panel
		}()).CanChooseFiles(true).CanChooseDirectories(false).
			SetTitle("添加文件").SetButtonText("添加").PromptForMultipleSelection()
	}
	pet.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); _ = s.SetVisible(false) })
	// AppKit owns the tray and pet context menus; Wails owns keyboard commands.
	// Drop Wails' unrelated File/View/Window/Help fixture menus.
	populate := func(menu *application.Menu) {
		menu.Add("打开聊天窗口").OnClick(func(*application.Context) { s.OpenHistory() })
		menu.Add("设置…").SetAccelerator("Cmd+,").OnClick(func(*application.Context) { s.OpenSettings() })
		menu.Add("检查更新…").OnClick(func(*application.Context) { s.OpenUpdates() })
		menu.Add("显示桌宠").OnClick(func(*application.Context) { logError(s.SetVisible(true)) })
		menu.Add("隐藏桌宠").OnClick(func(*application.Context) { logError(s.SetVisible(false)) })
		menu.AddSeparator()
		menu.Add("退出 Caelis Bot").SetAccelerator("Cmd+Q").OnClick(func(*application.Context) { quit() })
	}
	applicationMenu := app.Menu.New()
	populate(applicationMenu.AddSubmenu("Caelis Bot"))
	applicationMenu.AddRole(application.EditMenu)
	app.Menu.Set(applicationMenu)
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		s.start(newMacDriver(pet, panel, bubble, history, prop, s, quit))
		styleMacSettings(settings)
		runtime, err := bot.New(filepath.Join(root, "bot.json"), s.Gesture)
		if err != nil {
			logError(err)
			quit()
			return
		}
		companion.Store(runtime)
		runtime.SetReminderNotifier(func(id, label string) {
			if text := []rune(label); len(text) > 100 {
				label = string(text[:100]) + "…"
			}
			s.Notify(id, "到时间了", label, true)
		})
		bridge, err = bot.Serve(runtime)
		if err != nil {
			logError(err)
			quit()
			return
		}
		executable, err := os.Executable()
		if err != nil {
			logError(err)
			quit()
			return
		}
		if err = engine.ConfigureBotTools(bridge.Config(executable)); err != nil {
			logError(err)
			quit()
			return
		}
		back.SetBotStatus(runtime.Status)
		runtime.Start(engine)
		go func() {
			observer := backend.NotificationObserver{Notify: s.Notify}
			var revision uint64
			for {
				v, err := engine.WaitSnapshot(notificationContext, revision)
				if err != nil {
					return
				}
				revision = v.Revision
				observer.Observe(v)
				s.observeCharacter(v)
			}
		}()
		go func() { _ = back.Connect(context.Background()) }()
	})
	return app.Run()
}
func logError(err error) {
	if err != nil {
		log.Print(err)
	}
}
