package desktop

import (
	"crypto/rand"
	"golang.org/x/sys/windows/registry"
	"testing"
)

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
