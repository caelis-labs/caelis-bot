package desktop

import (
	"github.com/caelis-labs/caelis-bot/internal/windowsnotifications"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func (d *windowsDriver) startNotifications(root, name string) {
	if !currentWindowsShellIntegration() {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	roaming, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
	if err != nil {
		return
	}
	shortcutName := name
	base, _ := nativeDataDirectoryBase()
	if !strings.EqualFold(filepath.Clean(root), filepath.Join(base, name)) {
		id, _ := windowsnotifications.Identity(root)
		shortcutName += " (Test " + id[len("Caelis.Bot."):] + ")"
	}
	shortcut := filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs", name, shortcutName+".lnk")
	d.notifications = windowsnotifications.New(windowsnotifications.Config{Root: root, Executable: executable, Name: name, Shortcut: shortcut, Icon: appIcon}, func(err error) { log.Printf("Native notification unavailable: %v", err) })
}
func (d *windowsDriver) dismissNotification(id string) {
	application.InvokeAsync(func() {
		if d.notifications != nil && !d.stopped.Load() {
			d.notifications.Dismiss(id)
		}
	})
}
func (d *windowsDriver) notify(id, title, body string, reminder bool) {
	// Service.mu can be held here. Read native presentation on the UI thread
	// without waiting on it, and serialize delivery with later dismissals.
	application.InvokeAsync(func() {
		if d.notifications == nil || d.stopped.Load() {
			return
		}
		front, _, _ := winUser.NewProc("GetForegroundWindow").Call()
		chatFocused := d.history != nil && front == uintptr(d.history.NativeWindow())
		if !shouldDeliverNotification(chatFocused, d.placement.Visible, reminder) {
			return
		}
		d.notifications.Notify(id, title, body)
	})
}
func (d *windowsDriver) notificationStatus() string {
	if d.notifications == nil {
		return "unavailable"
	}
	return d.notifications.Status()
}
func (d *windowsDriver) configureNotifications() {
	go func() { _ = windowsOpenURL("ms-settings:notifications") }()
}

var _ notificationDriver = (*windowsDriver)(nil)
