//go:build windows && amd64

package windowsnotifications

import (
	"bytes"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	toast "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui/notifications"
	"golang.org/x/sys/windows"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

// An explicit live fixture uses an isolated AUMID and only synthetic content.
// Acceptance requires shell persistence, not just Show returning S_OK.
func TestWindowsLiveNotificationPersistsInShell(t *testing.T) {
	executable := os.Getenv("CAELIS_BOT_TEST_NOTIFICATION_EXE")
	if executable == "" {
		t.Skip("explicit native Bot executable required")
	}
	if !filepath.IsAbs(executable) || filepath.Base(executable) != "caelis-bot.exe" {
		t.Fatal("invalid Bot fixture path")
	}
	root := t.TempDir()
	roaming, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
	if err != nil {
		t.Fatal(err)
	}
	id, scheme := Identity(root)
	shortcut := filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs", "Caelis Bot notification acceptance", scheme+".lnk")
	var icon bytes.Buffer
	_ = png.Encode(&icon, image.NewNRGBA(image.Rect(0, 0, 32, 32)))
	manager := New(Config{Root: root, Executable: executable, Name: "Caelis Bot notification acceptance", Shortcut: shortcut, Icon: icon.Bytes()}, func(e error) { t.Log("Native notification diagnostic:", e) })
	defer func() {
		manager.Close()
		select {
		case <-manager.done:
		case <-time.After(time.Second):
			t.Error("notification actor did not stop")
		}
		_ = os.Remove(shortcut)
		_ = os.Remove(filepath.Dir(shortcut)) // Only an empty fixture folder.
		if err := Unregister(root, executable); err != nil {
			t.Error(err)
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for manager.Status() == "unavailable" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	done := make(chan error, 1)
	manager.enqueue(func() {
		err := manager.show("acceptance", "Caelis Bot Windows preview", "Native toast delivery test. Click opens chat only.")
		if err == nil {
			err = manager.show("retained", "Caelis Bot Windows preview", "Independent reminder retention test.")
		}
		done <- err
	})
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("native toast submission timeout")
	}
	deadline = time.Now().Add(10 * time.Second)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err = winrt.Initialize(); err != nil {
		t.Fatal(err)
	}
	defer winrt.Uninitialize()
	statics, err := toast.ToastNotificationManagerStatics2()
	if err != nil {
		t.Fatal(err)
	}
	defer statics.Release()
	history, err := statics.History()
	if err != nil {
		t.Fatal(err)
	}
	defer history.Release()
	defer history.ClearWithId(id)
	more, err := winrt.QueryInterface[toast.IToastNotificationHistory2](unsafe.Pointer(history), &toast.IID_IToastNotificationHistory2)
	if err != nil {
		t.Fatal(err)
	}
	defer more.Release()
	size := uint32(0)
	for time.Now().Before(deadline) {
		items, e := more.GetHistoryWithId(id)
		if e != nil {
			t.Fatal(e)
		}
		size, e = items.Size()
		items.Release()
		if e != nil {
			t.Fatal(e)
		}
		if size == 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if size != 2 {
		t.Fatal("toast not observed in native shell history", size)
	}
	manager.Dismiss("acceptance")
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, e := more.GetHistoryWithId(id)
		if e != nil {
			t.Fatal(e)
		}
		size, e = items.Size()
		items.Release()
		if e != nil {
			t.Fatal(e)
		}
		if size == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if size != 1 {
		t.Fatal("dismissal did not preserve independent notification", size)
	}
	t.Log("Two synthetic toasts persisted; dismissing one retained the other; popup/click acceptance remains separate")
}
