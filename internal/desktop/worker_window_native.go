//go:build (darwin && cgo) || windows

package desktop

import (
	"net/url"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// This registry owns presentation only. Closing a window drops its webview and
// reads, never the original Worker, native decisions, or task receipts.
func configureWorkerWindows(app *application.App, s *Service, focus func(*application.WebviewWindow), closed func(), display func(*application.WebviewWindow, DisplayState)) {
	windows := map[string]*application.WebviewWindow{}
	s.workerWindowsOpen = func() bool { return len(windows) > 0 }
	s.openWorkerTask = func(task api.Task) error {
		application.InvokeSync(func() {
			window := windows[task.ID]
			if window == nil {
				window = app.Window.NewWithOptions(application.WebviewWindowOptions{
					Name: "worker-" + task.ID, Title: task.Title + " [" + task.MachineName + "]",
					Width: 880, Height: 680, MinWidth: 360, MinHeight: 340,
					URL:            "/?surface=worker-task&task=" + url.QueryEscape(task.ID),
					BackgroundType: application.BackgroundTypeSolid,
				})
				windows[task.ID] = window
				closing := window
				window.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
					// Wails dispatches window hooks on an event goroutine.
					application.InvokeSync(func() {
						if windows[task.ID] != closing {
							return
						}
						delete(windows, task.ID)
						if closed != nil {
							closed()
						}
					})
				})
			}
			window.UnMinimise()
			window.Show()
			if display != nil {
				display(window, s.DisplayPreferences())
			}
			focus(window)
		})
		return nil
	}
	s.updateWorkerWindowTitle = func(id, title string) {
		application.InvokeSync(func() {
			if window := windows[id]; window != nil {
				window.SetTitle(title)
			}
		})
	}
	s.closeWorkerWindow = func(id string) {
		application.InvokeSync(func() {
			if window := windows[id]; window != nil {
				window.Close()
			}
		})
	}
	s.closeWorkerContext = func() bool {
		return application.InvokeSyncWithResult(func() bool {
			for _, window := range windows {
				if window.IsFocused() {
					window.Close()
					return true
				}
			}
			return false
		})
	}
	s.workerWindowsChanged = func(script string) {
		application.InvokeSync(func() {
			for _, window := range windows {
				if display != nil {
					display(window, s.DisplayPreferences())
				}
				window.ExecJS(script)
			}
		})
	}
}
