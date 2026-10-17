//go:build windows && amd64

package windowsnotifications

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	"github.com/deploymenttheory/go-bindings-win32/bindings/win32/foundation"
	com "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/com"
	storage "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/com/structuredstorage"
	"github.com/deploymenttheory/go-bindings-win32/bindings/win32/ui/shell"
	props "github.com/deploymenttheory/go-bindings-win32/bindings/win32/ui/shell/propertiessystem"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	dom "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/data/xml/dom"
	toast "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui/notifications"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type Manager struct {
	requests            chan func()
	status              atomic.Value
	closed              atomic.Bool
	config              Config
	appID, scheme, logo string
	report              func(error)
	lastError           string // actor-owned; diagnostics never include toast content
	done                chan struct{}
}

// One pinned MTA owns all WinRT references; callbacks never hold Service's lock.
func New(c Config, report func(error)) *Manager {
	id, scheme := Identity(c.Root)
	m := &Manager{requests: make(chan func(), 32), done: make(chan struct{}), config: c, appID: id, scheme: scheme, report: report}
	m.status.Store("unavailable")
	go func() {
		defer close(m.done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := winrt.Initialize(); err != nil {
			if report != nil {
				report(err)
			}
			return
		}
		defer winrt.Uninitialize()
		if err := m.register(); err != nil {
			if report != nil {
				report(err)
			}
			return
		}
		m.refresh()
		for fn := range m.requests {
			if m.closed.Load() {
				return
			}
			fn()
		}
	}()
	return m
}
func (m *Manager) enqueue(fn func()) {
	if !m.closed.Load() {
		select {
		case m.requests <- fn:
		default:
		}
	}
}
func (m *Manager) Close() {
	m.closed.Store(true)
	select {
	case m.requests <- func() {}:
	default:
	}
	select {
	case <-m.done:
	case <-time.After(2 * time.Second):
	}
}
func (m *Manager) Status() string { m.enqueue(m.refresh); return m.status.Load().(string) }
func (m *Manager) notifier() (*toast.IToastNotifier, error) {
	statics, err := toast.ToastNotificationManagerStatics()
	if err != nil {
		return nil, err
	}
	defer statics.Release()
	return statics.CreateToastNotifierWithId(m.appID)
}
func (m *Manager) refresh() {
	n, err := m.notifier()
	if err != nil {
		m.unavailable(fmt.Errorf("create notifier: %w", err))
		return
	}
	defer n.Release()
	setting, err := n.Setting()
	if err != nil {
		var code win32.HRESULT
		if errors.As(err, &code) && uint32(code) == 0x80070490 {
			// A fresh registration can lack a Settings entry until first delivery.
			// Keep OS state unknown; an explicit test can submit a real toast.
			m.status.Store("unknown")
			return
		}
		m.unavailable(fmt.Errorf("notification settings: %w", err))
		return
	}
	switch setting {
	case toast.NotificationSettingEnabled:
		m.status.Store("authorized")
	case toast.NotificationSettingDisabledByManifest:
		m.status.Store("unsupported")
	default:
		m.status.Store("denied")
	}
}

func (m *Manager) unavailable(err error) {
	m.status.Store("unavailable")
	if m.lastError != err.Error() && !m.closed.Load() && m.report != nil {
		m.lastError = err.Error()
		m.report(err)
	}
}
func (m *Manager) Notify(id, title, body string) {
	m.enqueue(func() {
		if err := m.show(id, title, body); err != nil && !m.closed.Load() && m.report != nil {
			m.report(err)
		}
	})
}

// Retire only this notification's original tag, keeping other reminders and
// approval notices in the same app/profile intact.
func (m *Manager) Dismiss(id string) {
	m.enqueue(func() {
		statics, err := toast.ToastNotificationManagerStatics2()
		if err != nil {
			m.unavailable(err)
			return
		}
		defer statics.Release()
		history, err := statics.History()
		if err != nil {
			m.unavailable(err)
			return
		}
		defer history.Release()
		sum := sha256.Sum256([]byte(id))
		if err = history.RemoveGroupedTagWithId(hex.EncodeToString(sum[:8]), "bot", m.appID); err != nil {
			m.unavailable(err)
		}
	})
}
func (m *Manager) show(id, title, body string) error {
	content, err := Payload(m.scheme, title, body, m.logo)
	if err != nil {
		return err
	}
	document, err := dom.NewXmlDocument()
	if err != nil {
		return err
	}
	defer document.Release()
	io, err := document.AsXmlDocumentIO()
	if err != nil {
		return err
	}
	defer io.Release()
	if err = io.LoadXml(content); err != nil {
		return err
	}
	notification, err := toast.CreateToastNotification(&document.IXmlDocument)
	if err != nil {
		return err
	}
	defer notification.Release()
	tagged, err := notification.AsToastNotification2()
	if err != nil {
		return err
	}
	defer tagged.Release()
	sum := sha256.Sum256([]byte(id))
	if err = tagged.SetTag(hex.EncodeToString(sum[:8])); err != nil {
		return err
	}
	if err = tagged.SetGroup("bot"); err != nil {
		return err
	}
	n, err := m.notifier()
	if err != nil {
		return err
	}
	defer n.Release()
	return n.Show(&notification.IToastNotification)
}

func (m *Manager) register() error {
	c := m.config
	if !filepath.IsAbs(c.Root) || !filepath.IsAbs(c.Executable) || !filepath.IsAbs(c.Shortcut) || filepath.Ext(c.Executable) != ".exe" {
		return errors.New("invalid notification registration")
	}
	directory := filepath.Join(c.Root, "Notifications")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if err := localstate.ProtectDirectory(directory); err != nil {
		return err
	}
	icon := filepath.Join(directory, "icon.png")
	if err := os.WriteFile(icon, c.Icon, 0600); err != nil {
		return err
	}
	m.logo = (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(icon)}).String()
	// A stub CLSID with protocol activation permits Action Center persistence
	// without turning Bot into a COM executable server. All activation args are fixed.
	for path, values := range map[string]map[string]string{
		`Software\Classes\AppUserModelId\` + m.appID:           {"DisplayName": c.Name, "IconUri": icon, "CustomActivator": "{F97C0785-C5C3-4C1D-9A77-81E079CD468F}", "CaelisOwner": c.Executable},
		`Software\Classes\` + m.scheme:                         {"": "URL:Caelis Bot chat", "URL Protocol": "", "CaelisOwner": c.Executable},
		`Software\Classes\` + m.scheme + `\shell\open\command`: {"": syscall.EscapeArg(c.Executable) + " --notification-chat " + syscall.EscapeArg(c.Root) + ` "%1"`},
	} {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			return err
		}
		for name, value := range values {
			if err = k.SetStringValue(name, value); err != nil {
				k.Close()
				return err
			}
		}
		k.Close()
	}
	if err := shell.SetCurrentProcessExplicitAppUserModelID(m.appID); err != nil {
		return err
	}
	if err := notificationShortcut(c.Shortcut, c.Executable, m.appID); err != nil {
		return err
	}
	path, err := windows.UTF16PtrFromString(c.Shortcut)
	if err != nil {
		return err
	}
	shell.SHChangeNotify(0x00000002, 5, unsafe.Pointer(path), nil)
	runtime.KeepAlive(path)
	return nil
}

func notificationShortcut(path, exe, id string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	clsid := win32.GUID{Data1: 0x00021401, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	var unknown *win32.IUnknown
	if err := com.CoCreateInstance(&clsid, nil, com.CLSCTX_INPROC_SERVER, &shell.IID_IShellLinkW, &unknown); err != nil {
		return err
	}
	defer unknown.Release()
	link := (*shell.IShellLinkW)(unsafe.Pointer(unknown))
	if err := link.SetPath(exe); err != nil {
		return err
	}
	store, err := winrt.QueryInterface[props.IPropertyStore](unsafe.Pointer(link), &props.IID_IPropertyStore)
	if err != nil {
		return err
	}
	defer store.Release()
	value, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return err
	}
	// Borrowed PROPVARIANT VT_LPWSTR; SetValue copies it. Keep the string alive.
	var prop storage.PROPVARIANT
	*(*uint16)(unsafe.Pointer(&prop)) = 31
	*(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(&prop), 8)) = unsafe.Pointer(value)
	format := win32.GUID{Data1: 0x9f4c2855, Data2: 0x9f79, Data3: 0x4b39, Data4: [8]byte{0xa8, 0xd0, 0xe1, 0xd4, 0x2d, 0xe1, 0xd5, 0xf3}}
	key := foundation.PROPERTYKEY{Fmtid: format, Pid: 5}
	err = store.SetValue(&key, &prop)
	runtime.KeepAlive(value)
	if err != nil {
		return err
	}
	stub := win32.GUID{Data1: 0xf97c0785, Data2: 0xc5c3, Data3: 0x4c1d, Data4: [8]byte{0x9a, 0x77, 0x81, 0xe0, 0x79, 0xcd, 0x46, 0x8f}}
	*(*uint16)(unsafe.Pointer(&prop)) = 72
	*(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(&prop), 8)) = unsafe.Pointer(&stub)
	key.Pid = 26
	err = store.SetValue(&key, &prop)
	runtime.KeepAlive(&stub)
	if err != nil {
		return err
	}
	if err = store.Commit(); err != nil {
		return err
	}
	persist, err := winrt.QueryInterface[com.IPersistFile](unsafe.Pointer(link), &com.IID_IPersistFile)
	if err != nil {
		return err
	}
	defer persist.Release()
	return persist.Save(path, true)
}
