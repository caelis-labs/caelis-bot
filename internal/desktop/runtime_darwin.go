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
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/contentpack"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"github.com/caelis-labs/caelis-bot/internal/updates"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed assets/app-icon.png
var appIcon []byte

func Run(assets fs.FS) error {
	root, err := applicationDataDirectory()
	if err != nil {
		return err
	}
	diagnostics := diagnosticlog.New(filepath.Join(root, "Logs"))
	env, envErr := runtimeenv.Resolve(context.Background(), os.Environ())
	if err := runtimeenv.Install(env); err != nil {
		envErr = err
	}
	if envErr != nil {
		// Resolve returns only locally generated causes, never shell output/values.
		diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "environment", Code: "shell_environment_failed", Reason: envErr.Error()})
	} else {
		diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "environment", Code: "shell_environment_loaded", Reason: "user login and interactive shell exports loaded for native runtimes"})
	}
	s := newService(fileStore{filepath.Join(root, "placement.json")})
	s.configureShortcut(filepath.Join(root, "shortcut.json"))
	s.content, err = contentpack.NewRegistry(filepath.Join(root, "content"))
	if err != nil {
		logError(err)
	}

	core, err := app.New(root, app.Host{Diagnostics: diagnostics, ResolveFiles: s.resolveDraftFiles, ConsumeFiles: s.consumeDraftFiles,
		OpenURL:    func(url string) error { return exec.Command("/usr/bin/open", url).Run() },
		RevealFile: func(path string) error { return exec.Command("/usr/bin/open", "-R", path).Run() },
		TrashFile:  trashNativePath, Gesture: s.Gesture, Notify: s.Notify, Observe: s.observeCharacter, ReportError: logError})
	if err != nil {
		return err
	}
	defer core.Close()
	back := core.Backend
	s.needsIntroduction = func() bool {
		v := back.BotInitialization()
		return v.Required || v.Status == "rejected" || v.Status == "unknown"
	}
	logError(s.configureSelection(filepath.Join(core.ProviderDirectory(), "draft-files.json")))
	s.storage, s.cleanStorage = core.AttachmentStorage, core.CleanAttachments
	s.diagnosticReport = back.DiagnosticReport
	s.activate = func() {
		if core.NeedsSetup() || !core.HasRuntimeChoice() {
			s.showSettings("setup")
			return
		}
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
	var nativeApp *application.App
	var quitting, finished atomic.Bool
	quit := func() {
		if quitting.CompareAndSwap(false, true) {
			go func() {
				logError(core.Close())
				finished.Store(true)
				nativeApp.Quit()
			}()
		}
	}
	assetHandler := application.AssetFileServerFS(assets)
	if s.content != nil {
		assetHandler = s.content.Handler(assetHandler)
	}
	nativeApp = application.New(application.Options{
		Name: "Caelis Bot", Description: "A quiet desktop companion",
		Icon:                        appIcon,
		Assets:                      application.AssetOptions{Handler: assetHandler},
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
			stopMacUpdater()
			logError(core.Close())
			s.shutdown()
		},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "dev.caelis.bot", OnSecondInstanceLaunch: func(application.SecondInstanceData) { _ = s.SetVisible(true) }},
	})
	signals := make(chan os.Signal, 1)
	s.copyText = nativeApp.Clipboard.SetText
	s.restartRuntime = func() error {
		if err := core.PrepareRestart(); err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			back.CancelRestart()
			return err
		}
		// A separate helper waits for this exact owner to exit before LaunchServices
		// recalls the bundle; launching earlier would hit the single-instance lock.
		script := `while kill -0 "$1" 2>/dev/null; do sleep 0.1; done; exec "$2" --env "CAELIS_BOT_DATA_DIR=$4" "$3"`
		target := executable
		launcher := executable
		if i := strings.LastIndex(executable, ".app/Contents/MacOS/"); i >= 0 {
			target = executable[:i+4]
			launcher = "/usr/bin/open"
		} else {
			script = `while kill -0 "$1" 2>/dev/null; do sleep 0.1; done; exec "$2"`
		}
		helper := exec.Command("/bin/sh", "-c", script, "caelis-relaunch", strconv.Itoa(os.Getpid()), launcher, target, root)
		if err = helper.Start(); err != nil {
			back.CancelRestart()
			return err
		}
		go func() { _ = helper.Wait() }()
		quit()
		return nil
	}
	s.openReleasePage = func() error { return exec.Command("/usr/bin/open", updates.ReleasePage).Run() }
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() { <-signals; quit() }()
	pet := nativeApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "pet", Title: "Caelis Bot — 桌宠", Width: 180, Height: 240, Frameless: true, DisableResize: true, Hidden: true,
		IgnoreMouseEvents: true, URL: "/?surface=pet", BackgroundType: application.BackgroundTypeTransparent,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, DisableShadow: true, CornerType: application.MacWindowCornerTypeSquare,
			WindowLevel: application.MacWindowLevelFloating, CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces | application.MacWindowCollectionBehaviorStationary | application.MacWindowCollectionBehaviorFullScreenAuxiliary | application.MacWindowCollectionBehaviorIgnoresCycle},
	})
	panel := nativeApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "conversation", Title: "Caelis Bot", Width: 420, Height: 64, Frameless: true, DisableResize: true, Hidden: true,
		URL: "/?surface=panel", BackgroundType: application.BackgroundTypeTransparent, EnableFileDrop: true,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, CornerType: application.MacWindowCornerTypeSquare, WindowLevel: application.MacWindowLevelFloating, CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace | application.MacWindowCollectionBehaviorFullScreenAuxiliary},
	})
	history := nativeApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "history", Title: "Caelis Bot", Width: 640, Height: 700, MinWidth: 420, MinHeight: 360,
		Hidden: true, URL: "/?surface=history", EnableFileDrop: true, BackgroundColour: application.NewRGB(247, 247, 247),
		Mac: application.MacWindow{TitleBar: application.MacTitleBar{AppearsTransparent: true}},
	})
	bubble := nativeApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "bubble", Title: "Caelis Bot — 消息", Width: 360, Height: 68, Frameless: true, DisableResize: true, Hidden: true,
		URL: "/?surface=bubble", BackgroundType: application.BackgroundTypeTransparent,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, DisableShadow: true},
	})
	prop := nativeApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "prop", Title: "Caelis Bot — 纸飞机", Width: 520, Height: 360, Frameless: true, DisableResize: true, Hidden: true,
		URL: "/?surface=prop", BackgroundType: application.BackgroundTypeTransparent,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, DisableShadow: true},
	})
	settings := nativeApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "settings", Title: "Caelis Bot — 设置", Width: 1040, Height: 710, MinWidth: 860, MinHeight: 640,
		Hidden: true, URL: "/?surface=settings", BackgroundType: application.BackgroundTypeTransparent,
		Mac: application.MacWindow{Backdrop: application.MacBackdropTransparent, TitleBar: application.MacTitleBar{AppearsTransparent: true}},
	})
	for _, window := range []*application.WebviewWindow{panel, bubble, settings} {
		window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) { syncMacMaterials() })
	}
	s.openSettings = func() {
		if !s.prepareWindowRecall() {
			return
		}
		application.InvokeSync(func() {
			syncMacDock(history, settings, true)
			settings.UnMinimise()
			settings.Show()
			settings.Focus()
			settings.ExecJS("window.dispatchEvent(new Event('settings-open'))")
		})
	}
	s.closeSettings = func() {
		application.InvokeSync(func() {
			settings.ExecJS("window.dispatchEvent(new Event('settings-close'))")
			settings.Hide()
			syncMacDock(history, settings, false)
		})
	}
	s.saveDiagnosticPath = func() (string, error) {
		return nativeApp.Dialog.SaveFile().AttachToWindow(settings).SetFilename("Caelis-Bot-diagnostics.json").
			SetMessage("仅包含系统、连接和状态计数；不包含聊天内容、文件路径或凭据。").
			AddFilter("JSON", "*.json").CanCreateDirectories(true).PromptForSingleSelection()
	}

	s.pickRuntimeCLI = func() (string, error) {
		return nativeApp.Dialog.OpenFile().AttachToWindow(settings).CanChooseFiles(true).CanChooseDirectories(false).SetTitle("选择运行时可执行文件").SetButtonText("选择").PromptForSingleSelection()
	}
	s.pickContentFile = func() (string, error) {
		return nativeApp.Dialog.OpenFile().AttachToWindow(settings).CanChooseFiles(true).CanChooseDirectories(false).AddFilter("Caelis 内容包", "*.caelispack").SetTitle("导入内容包").SetButtonText("导入").PromptForSingleSelection()
	}
	s.contentChanged = func(value contentpack.Appearance) {
		data, _ := json.Marshal(value)
		for _, window := range []*application.WebviewWindow{pet, panel, history, bubble, settings} {
			window.ExecJS("window.dispatchEvent(new CustomEvent('appearance-changed',{detail:" + string(data) + "}))")
		}
	}
	settings.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); s.closeSettings() })
	showHistory := func() {
		syncMacDock(history, settings, true)
		history.UnMinimise()
		history.Show()
		history.Focus()
		history.ExecJS("window.dispatchEvent(new Event('history-open'))")
	}
	s.openHistory = func() {
		if s.prepareWindowRecall() {
			application.InvokeSync(showHistory)
		}
	}
	s.closeHistory = func() {
		application.InvokeSync(func() {
			history.ExecJS("window.dispatchEvent(new Event('history-close'))")
			history.Hide()
			syncMacDock(history, settings, false)
		})
	}
	s.historyVisible = func() bool { return macWindowVisible(history) }
	s.historyCanHide = func() bool { return macWindowCanHide(history) }
	s.recallWindows = func() {
		if !s.prepareWindowRecall() {
			return
		}
		application.InvokeSync(func() {
			// Recall existing contextual windows, including minimised settings,
			// without reopening a page the user explicitly closed. Settings is
			// raised last so the larger chat window cannot cover its controls.
			settingsOpen := macWindowOpen(settings)
			showHistory()
			if settingsOpen {
				settings.UnMinimise()
				settings.Show()
				settings.Focus()
			}
			if os.Getenv("CAELIS_BOT_DESKTOP_TRACE") != "" {
				log.Printf("Desktop recall: history=%t settings-open=%t settings=%t settings-focused=%t", macWindowVisible(history), settingsOpen, macWindowVisible(settings), settings.IsFocused())
			}
		})
	}
	history.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); s.closeHistory() })
	// Wails' default Dock callback reveals every hidden window, including the
	// private pet/prop/composer webviews. Cancel it and recall only open panels.
	nativeApp.Event.RegisterApplicationEventHook(events.Mac.ApplicationShouldHandleReopen, func(e *application.ApplicationEvent) {
		e.Cancel()
		if !s.prepareWindowRecall() {
			return
		}
		application.InvokeSync(func() {
			if macWindowOpen(history) {
				showHistory()
			}
			if macWindowOpen(settings) {
				syncMacDock(history, settings, true)
				settings.UnMinimise()
				settings.Show()
				settings.Focus()
			}
		})
	})
	for _, window := range []*application.WebviewWindow{history, settings} {
		window.OnWindowEvent(events.Mac.WindowDidBecomeKey, func(*application.WindowEvent) {
			// AppKit can reveal a window without Wails.Show (for example, an
			// accessibility activation). Wails otherwise keeps Hidden=true and
			// discards the native close button before our closing hook sees it.
			application.InvokeSync(func() {
				if !window.IsFocused() {
					return // A queued activation must not reopen or refocus a window.
				}
				window.Show()
				syncMacDock(history, settings, false)
				if window == history {
					// Ordinary app focus must not reset an existing history read.
					history.ExecJS("window.dispatchEvent(new Event('history-visible'))")
				}
			})
		})
	}
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
		return nativeApp.Dialog.OpenFile().AttachToWindow(func() *application.WebviewWindow {
			if macWindowVisible(history) {
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
		menu.Add("退出").SetAccelerator("Cmd+Q").OnClick(func(*application.Context) { quit() })
	}
	applicationMenu := nativeApp.Menu.New()
	populate(applicationMenu.AddSubmenu("Caelis Bot"))
	applicationMenu.AddRole(application.EditMenu)
	nativeApp.Menu.Set(applicationMenu)
	nativeApp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if !installMacAppIcon() {
			log.Print("Desktop application icon could not be decoded")
		}
		s.start(newMacDriver(pet, panel, bubble, history, prop, s, quit))
		styleMacSettings(settings)
		startMacUpdater(s, core.PrepareUpdate, core.CancelUpdate, func() {
			quitting.Store(true)
			logError(core.Close())
			finished.Store(true)
		})
		if err := core.PreparePersonal(); err != nil {
			logError(err)
			quit()
			return
		}
		if core.NeedsSetup() {
			s.showSettings("setup")
		}
		if !core.HasRuntimeChoice() {
			return
		}
		if err := core.Start(); err != nil {
			logError(err)
			quit()
		}
	})
	return nativeApp.Run()
}
func logError(err error) {
	if err != nil {
		log.Print(err)
	}
}
