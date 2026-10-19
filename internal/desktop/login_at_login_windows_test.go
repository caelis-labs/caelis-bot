package desktop

import (
	"crypto/rand"
	"errors"
	"golang.org/x/sys/windows/registry"
	"testing"
)

func TestWindowsZIPLoginMigrationToSetup(t *testing.T) {
	path := `Software\Caelis Bot\Tests\` + rand.Text()
	defer registry.DeleteKey(registry.CURRENT_USER, path)
	oldZIP := &windowsLoginAtLogin{path: path, name: "preview", command: `"C:\ZIP\caelis-bot.exe" --login-launch`}
	zip := *oldZIP
	zip.disableOnly = true
	// A clean ZIP must neither create the key nor offer an enable control.
	if zip.Status().Supported || zip.Set(true) == nil || zip.Set(false) != nil {
		t.Fatal("clean ZIP allowed startup registration")
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE); !errors.Is(err, registry.ErrNotExist) {
		k.Close()
		t.Fatal("ZIP created a registry key", err)
	}
	if err := oldZIP.Set(true); err != nil {
		t.Fatal(err)
	}
	service := &Service{loginAtLogin: &zip}
	if !service.LoginAtLoginStatus().Enabled {
		t.Fatal("historical ZIP entry cannot be disabled in settings")
	}
	if status, err := service.SetLoginAtLogin(false); err != nil || status.Supported || status.Registered {
		t.Fatal("historical ZIP entry was not removed", status, err)
	}
	if _, err := service.SetLoginAtLogin(true); err == nil {
		t.Fatal("ZIP re-enabled startup")
	}
	setup := &windowsLoginAtLogin{path: path, name: zip.name, command: `"C:\Program Files\Caelis Bot\caelis-bot.exe" --login-launch`}
	if err := setup.Set(true); err != nil {
		t.Fatal("Setup could not register after ZIP migration", err)
	}
	if zip.Status().Supported || zip.Set(false) == nil || zip.Set(true) == nil || !setup.Status().Enabled {
		t.Fatal("ZIP changed another installation's registration")
	}
	// Even the same executable with extra arguments is not our exact command.
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err := k.SetStringValue(zip.name, zip.command+" --other"); err != nil {
		t.Fatal(err)
	}
	if zip.Status().Supported || zip.Set(false) == nil {
		t.Fatal("ZIP accepted a partial command match")
	}
}

func TestWindowsLoginRegistrationPreservesForeignOwner(t *testing.T) {
	path := `Software\Caelis Bot\Tests\` + rand.Text()
	k, _, e := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if e != nil {
		t.Fatal(e)
	}
	defer registry.DeleteKey(registry.CURRENT_USER, path)
	defer k.Close()
	m := &windowsLoginAtLogin{path: path, name: "preview", command: `"C:\QA 中文\caelis-bot.exe" --login-launch`}
	if v := m.Status(); v.Registered || !v.Supported {
		t.Fatal(v)
	}
	if e = m.Set(true); e != nil {
		t.Fatal(e)
	}
	if v := m.Status(); !v.Registered || !v.Enabled || v.State != "registered" {
		t.Fatal(v)
	}
	if e = m.Set(false); e != nil {
		t.Fatal(e)
	}
	if v := m.Status(); v.Registered {
		t.Fatal(v)
	}
	if e = k.SetStringValue(m.name, "foreign command"); e != nil {
		t.Fatal(e)
	}
	if m.Set(false) == nil || m.Set(true) == nil {
		t.Fatal("foreign startup entry overwritten")
	}
	if v, _, _ := k.GetStringValue(m.name); v != "foreign command" {
		t.Fatal("foreign entry changed")
	}
}
