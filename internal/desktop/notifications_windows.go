package desktop

import (
	"github.com/caelis-labs/caelis-bot/internal/windowsnotifications"
	"golang.org/x/sys/windows"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func (d *windowsDriver) startNotifications(root, name string) {
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
	if d.notifications != nil && !d.stopped.Load() {
		d.notifications.Dismiss(id)
	}
}
func (d *windowsDriver) notify(id, title, body string, _ bool) {
	if d.notifications != nil && !d.stopped.Load() {
		d.notifications.Notify(id, title, body)
	}
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
