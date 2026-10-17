package desktop

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/appicon"
	"github.com/caelis-labs/caelis-bot/internal/contentpack"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"github.com/caelis-labs/caelis-bot/internal/updates"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed assets/app-icon.png
var appIcon []byte

// Run owns only the Windows shell. Product state, backend assembly and the
// retained contextual views use the same contracts as the shipped Mac host.
func Run(assets fs.FS) error {
	if assets == nil {
		return errors.New("embedded product assets unavailable")
	}
	if err := windowsCompatibility(); err != nil {
		return err
	}
	env, _, err := runtimeenv.ResolveWithReport(context.Background(), os.Environ(), environmentNotebookHomes()...)
	if err != nil {
		return err
	}
	if err = runtimeenv.Install(env); err != nil {
		return err
	}
	root, err := applicationDataDirectory()
	if err != nil {
		return err
	}
	if err = localstate.EnsurePrivateDirectory(root); err != nil {
		return err
	}
	assembly, err := newProductAssembly(root, windowsPreferredLanguages(), func(*Service) app.Host {
		return app.Host{OpenURL: windowsOpenURL, RevealFile: windowsRevealFile, TrashFile: windowsTrashFile, CareSample: windowsCareSample}
	})
	if err != nil {
		return err
	}
	s, core := assembly.Service, assembly.Core
	defer core.Close()
	s.loginAtLogin = newWindowsLoginAtLogin()
	s.readClipboard = readWindowsClipboard
	s.terminalChoices = taskterminal.WindowsChoices
	terminalGate := &windowsTerminalGate{}
	openConsole := func(ctx context.Context, path string) (taskterminal.Window, error) {
		return terminalGate.launch(ctx, func() (taskterminal.Window, error) {
			preferences, err := s.TaskPreferences()
			if err != nil {
				return nil, taskterminal.NotLaunched(err)
			}
			switch preferences.Terminal {
			case "system":
				return taskterminal.LaunchConsole(ctx, path)
			case "windows-console":
				return taskterminal.LaunchClassicConsole(ctx, path)
			case "windows-terminal":
				return taskterminal.LaunchHostedConsole(ctx, "wt.exe -w -1 -- {script}", path)
			case "custom":
				return taskterminal.LaunchHostedConsole(ctx, preferences.CustomCommand, path)
			default:
				return nil, taskterminal.NotLaunched(taskterminal.ErrWindowUnsupported)
			}
		})
	}
	terminalWindows := taskterminal.NewWindowManager(filepath.Join(root, "Terminal"), openConsole, core.WorkTerminal, s.taskWindowChanged, taskterminal.InputEpoch)
	defer terminalWindows.Close()
	s.taskWindows = terminalWindows
	s.observeTaskTerminal = func(ctx context.Context, id string) { s.observeWindowsTaskTerminal(ctx, id, terminalWindows) }
	s.taskError = func(id string, err error) {
		assembly.Diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "terminal", Code: "task_open_failed", Item: id, Reason: diagnosticlog.Reason(err.Error())})
	}
	// Remote SSH transport remains explicitly unsupported. Machine setup does
	// not borrow a local task's runtime endpoint when a remote route is absent.
	s.openArtifactFile = windowsOpenURL
	s.openReleasePage = func() error { return windowsOpenURL(updates.ReleasePage) }
	appName, _ := applicationIdentity()
	var native *application.App
	stopUpdater := func() {}
	var updaterMu sync.Mutex
	stopUpdates := func() { updaterMu.Lock(); defer updaterMu.Unlock(); stopUpdater() }
	defer stopUpdates()
	var quitting, finished atomic.Bool
	quit := func() {
		if quitting.CompareAndSwap(false, true) {
			go func() {
				stopUpdates()
				logError(core.Close())
				terminalWindows.CancelAll()
				terminalWindows.Close()
				s.shutdown()
				finished.Store(true)
				native.Quit()
			}()
		}
	}
	handler := application.AssetFileServerFS(assets)
	if s.content != nil {
		handler = s.content.Handler(handler)
	}
	native = application.New(application.Options{
		Name: appName, Description: "A quiet desktop companion", Icon: appIcon,
		Assets:                      application.AssetOptions{Handler: handler},
		Services:                    []application.Service{application.NewService(s), application.NewService(core.Backend)},
		Windows:                     application.WindowsOptions{DisableQuitOnLastWindowClosed: true, WebviewUserDataPath: filepath.Join(root, "WebView2")},
		DisableDefaultSignalHandler: true,
		ShouldQuit: func() bool {
			if finished.Load() {
				return true
			}
			quit()
			return false
		},
		OnShutdown: func() { logError(core.Close()); s.shutdown() },
		SingleInstance: &application.SingleInstanceOptions{UniqueID: applicationInstanceID(root), OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
			go func() {
				if !s.windowsLaunchAction(data.Args) {
					logError(s.SetVisible(true))
				}
			}()
		}},
	})
	s.copyText = native.Clipboard.SetText
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-signals:
			quit()
		case <-done:
		}
	}()
	// Assign the menu before Wails creates the native tray, so OpenMenu keeps
	// its owner and the renderer never gains quit/menu authority.
	tray := native.SystemTray.New()
	if icon, err := png.Decode(bytes.NewReader(appIcon)); err == nil {
		tray.SetIcon(appicon.ICO(icon))
	} else {
		return err
	}
	tray.SetTooltip(appName)
	populate := func(state LanguageState) {
		menu := native.Menu.New()
		label := func(key string) string { return i18n.Text(state.Locale, "native."+key, nil) }
		menu.Add(label("chat")).OnClick(func(*application.Context) { go s.OpenHistory() })
		visible := s.Placement().Visible
		key := "showPet"
		if visible {
			key = "hidePet"
		}
		menu.Add(label(key)).OnClick(func(*application.Context) { go func() { logError(s.SetVisible(!visible)) }() })
		menu.AddSeparator()
		capture := s.FeatureCapabilities().Capture
		menu.Add(label("capture.start")).SetEnabled(capture.Available && capture.Enabled).OnClick(func(*application.Context) { go s.CaptureScreen() })
		menu.Add(label("capture.paste")).SetEnabled(capture.Available && capture.Enabled).OnClick(func(*application.Context) { go s.PasteImage() })
		menu.Add(label("capture.togglePins")).SetEnabled(capture.Available && capture.Enabled).OnClick(func(*application.Context) { go s.ToggleImagePins() })
		menu.AddSeparator()
		menu.Add(label("settings")).OnClick(func(*application.Context) { go s.OpenSettings() })
		menu.Add(label("updates")).OnClick(func(*application.Context) { go s.OpenUpdates() })
		menu.AddSeparator()
		menu.Add(label("quit")).OnClick(func(*application.Context) { quit() })
		tray.SetMenu(menu)
	}
	populate(s.LanguagePreferences())
	openMenu := func() { go func() { populate(s.LanguagePreferences()); tray.OpenMenu() }() }
	tray.OnClick(openMenu)
	tray.OnRightClick(openMenu)
	// Native HWNDs must exist before installing pointer handling; delayed
	// creation also avoids reentrant pending WebView startup on Windows.
	native.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		pet := native.Window.NewWithOptions(application.WebviewWindowOptions{Name: "pet", Title: s.text("native.petTitle", nil), Width: 180, Height: 240, Frameless: true, DisableResize: true, Hidden: true,
			URL: "/?surface=pet", BackgroundType: application.BackgroundTypeTransparent, AlwaysOnTop: true,
			Windows: application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true, WebView2CompositionHosting: true, ExStyle: windowsPetStyle, WindowDidMoveDebounceMS: 120}})
		bubble := native.Window.NewWithOptions(application.WebviewWindowOptions{Name: "bubble", Title: s.text("native.bubbleTitle", nil), Width: 360, Height: 68, Frameless: true, DisableResize: true, Hidden: true,
			URL: "/?surface=bubble", BackgroundType: application.BackgroundTypeTransparent, AlwaysOnTop: true,
			Windows: application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true, WebView2CompositionHosting: true, ExStyle: windowsPetStyle}})
		history := native.Window.NewWithOptions(application.WebviewWindowOptions{Name: "history", Title: appName, Width: 640, Height: 700, MinWidth: 420, MinHeight: 360,
			Hidden: true, URL: "/?surface=context", EnableFileDrop: true, BackgroundColour: application.NewRGB(247, 247, 247)})
		settings := native.Window.NewWithOptions(application.WebviewWindowOptions{Name: "settings", Title: appName, Width: 1040, Height: 740, MinWidth: 520, MinHeight: 560,
			Hidden: true, URL: "/?surface=settings", BackgroundColour: application.NewRGB(247, 247, 247)})
		preview := native.Window.NewWithOptions(application.WebviewWindowOptions{Name: "artifact-preview", Title: appName, Width: 820, Height: 620, MinWidth: 520, MinHeight: 360,
			Hidden: true, URL: "/?surface=artifact-preview&chrome=web", BackgroundColour: application.NewRGB(247, 247, 247)})
		dock := native.Window.NewWithOptions(application.WebviewWindowOptions{Name: "tasks", Title: s.text("native.tasksTitle", nil), Width: 280, Height: 300,
			Frameless: true, DisableResize: true, Hidden: true, URL: "/?surface=windows-tasks", HideOnEscape: true, AlwaysOnTop: true,
			BackgroundType: application.BackgroundTypeTransparent,
			Windows:        application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true, WebView2CompositionHosting: true}})
		dock.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); dock.Hide() })
		windows := []*application.WebviewWindow{pet, bubble, history, settings, preview, dock}
		s.configureWindowsContext(native, appName, history, settings, preview)
		broadcast := func(event string, value any) {
			data, _ := json.Marshal(value)
			for _, window := range windows {
				window.ExecJS("window.dispatchEvent(new CustomEvent('" + event + "',{detail:" + string(data) + "}))")
			}
		}
		s.contentChanged = func(value contentpack.Appearance) { broadcast("appearance-changed", value) }
		s.displayChanged = func(value DisplayState) { broadcast("display-changed", value) }
		s.languageChanged = func(value LanguageState) { populate(value); broadcast("language-changed", value) }
		pet.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); go func() { logError(s.SetVisible(false)) }() })
		d, err := newWindowsDriver(native, pet, bubble, history, s)
		if err != nil {
			logError(err)
			quit()
			return
		}
		d.taskWindow = dock
		d.diagnostic = func(code, reason string) {
			assembly.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "desktop", Code: code, Reason: reason})
		}
		s.start(d)
		d.startNotifications(root, appName)
		updaterMu.Lock()
		stopUpdater = s.startWindowsUpdater(root, core, terminalGate.pause, terminalWindows.CloseClientsForUpdate, quit, func() {
			s.Notify("windows-update-ready", appName, s.text("native.windowsUpdateReady", nil), false)
		})
		if quitting.Load() {
			stopUpdater()
		}
		updaterMu.Unlock()
		atLogin := false
		for _, arg := range os.Args[1:] {
			atLogin = atLogin || arg == "--login-launch"
		}
		logError(assembly.StartForLaunch(atLogin))
		if !atLogin {
			go s.windowsLaunchAction(os.Args[1:])
		}
		d.diagnostic("native_ready", "Windows contextual shell initialized")
		go d.capture.prewarm()
	})
	return native.Run()
}

func (s *Service) configureWindowsContext(native *application.App, name string, history, settings, preview *application.WebviewWindow) {
	// Presentation tokens are confined to the UI thread. Closing a panel
	// invalidates delayed renderer acknowledgements without cancelling work.
	var presentation uint64
	var settingsMode, setupMode atomic.Bool
	prepare := func(window *application.WebviewWindow, surface string) {
		presentation++
		window.ExecJS(fmt.Sprintf("(function prepare(){if(window.prepareSurface)window.prepareSurface(%q,%d);else setTimeout(prepare,50)})()", surface, presentation))
	}
	s.surfaceReady = func(surface string, token uint64) {
		application.InvokeSync(func() {
			if token != presentation || (surface != "chat" && surface != "settings") {
				return
			}
			target, previous, event := history, settings, "settings-close"
			if surface == "settings" {
				target, previous, event = settings, history, "history-close"
			}
			target.UnMinimise()
			showWindowsWindow(target)
			target.Focus()
			previous.Hide()
			previous.ExecJS("window.dispatchEvent(new Event('" + event + "'))")
		})
	}
	showHistory := func() { settingsMode.Store(false); prepare(history, "chat") }
	s.openHistory = func() {
		if s.prepareWindowRecall() {
			application.InvokeSync(showHistory)
		}
	}
	s.openSettings = func() {
		if !s.prepareWindowRecall() {
			return
		}
		s.presentSettings(application.InvokeSync, func(welcome bool, title string) {
			settingsMode.Store(true)
			if setupMode.Swap(welcome) != welcome {
				settings.Hide()
				if welcome {
					settings.SetSize(640, 660)
				} else {
					settings.SetSize(1040, 740)
				}
			}
			settings.SetTitle(strings.Replace(title, "Caelis Bot", name, 1))
			prepare(settings, "settings")
		})
	}
	s.closeHistory = func() {
		application.InvokeSync(func() {
			if settingsMode.Load() {
				return
			}
			presentation++
			history.ExecJS("window.dispatchEvent(new Event('history-close'))")
			history.Hide()
		})
	}
	s.closeSettings = func() {
		application.InvokeSync(func() {
			if !settingsMode.Load() {
				return
			}
			presentation++
			settings.ExecJS("window.dispatchEvent(new Event('settings-close'))")
			settings.Hide()
		})
	}
	s.settingsVisible = func() bool { return settingsMode.Load() && settings.IsVisible() }
	s.historyVisible = func() bool { return !settingsMode.Load() && history.IsVisible() }
	s.historyCanHide = func() bool { return !settingsMode.Load() && history.IsVisible() && !history.IsMinimised() }
	s.petChatCanHide = s.historyCanHide
	s.recallWindows = func() {
		if !s.prepareWindowRecall() {
			return
		}
		application.InvokeSync(func() {
			if settingsMode.Load() && settings.IsVisible() {
				settings.UnMinimise()
				showWindowsWindow(settings)
				settings.Focus()
				settings.ExecJS("window.dispatchEvent(new Event('settings-open'))")
			} else {
				showHistory()
			}
		})
	}
	history.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); go s.CloseHistory() })
	settings.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); go s.CloseSettings() })
	preview.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); preview.Hide() })
	s.openArtifactPreview = func() {
		application.InvokeSync(func() {
			selection, err := s.CurrentArtifactPreview()
			if err != nil {
				return
			}
			preview.SetTitle(selection.Name)
			preview.UnMinimise()
			showWindowsWindow(preview)
			preview.Focus()
			preview.ExecJS("window.dispatchEvent(new Event('artifact-preview-changed'))")
		})
	}
	s.pickFiles = func() ([]string, error) {
		return native.Dialog.OpenFile().AttachToWindow(history).CanChooseFiles(true).CanChooseDirectories(false).SetTitle(s.text("native.addFileTitle", nil)).PromptForMultipleSelection()
	}
	s.pickRuntimeCLI = func() (string, error) {
		return native.Dialog.OpenFile().AttachToWindow(settings).CanChooseFiles(true).AddFilter("Program", "*.exe").PromptForSingleSelection()
	}
	s.pickSSHKey = func() (string, error) {
		return native.Dialog.OpenFile().AttachToWindow(settings).CanChooseFiles(true).ShowHiddenFiles(true).PromptForSingleSelection()
	}
	s.pickContentFile = func() (string, error) {
		return native.Dialog.OpenFile().AttachToWindow(settings).CanChooseFiles(true).AddFilter(s.text("native.contentPackFilter", nil), "*.caelispack").PromptForSingleSelection()
	}
	s.saveDiagnosticPath = func() (string, error) {
		return native.Dialog.SaveFile().AttachToWindow(settings).SetFilename("Caelis-Bot-diagnostics.json").AddFilter("JSON", "*.json").PromptForSingleSelection()
	}
	s.saveArtifactPath = func(name string) (string, error) {
		owner := history
		if preview.IsFocused() {
			owner = preview
		}
		return native.Dialog.SaveFile().AttachToWindow(owner).SetFilename(name).CanCreateDirectories(true).PromptForSingleSelection()
	}
	s.filesChanged = func() { history.ExecJS("window.dispatchEvent(new Event('files-changed'))") }
	history.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		paths := append([]string(nil), e.Context().DroppedFiles()...)
		go func() {
			s.mu.Lock()
			before := len(s.files)
			_, err := s.stageFiles(paths)
			added := len(s.files) - before
			s.mu.Unlock()
			message := ""
			if err != nil {
				message = err.Error()
			}
			data, _ := json.Marshal(map[string]any{"error": message, "added": added})
			history.ExecJS("window.dispatchEvent(new CustomEvent('files-changed',{detail:" + string(data) + "}))")
		}()
	})
}

func (s *Service) windowsLaunchAction(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--chat":
			s.OpenHistory()
		case "--settings":
			s.OpenSettings()
		case "--updates":
			s.OpenUpdates()
		case "--capture":
			s.CaptureScreen()
		case "--paste-image":
			s.PasteImage()
		default:
			continue
		}
		return true
	}
	return false
}
