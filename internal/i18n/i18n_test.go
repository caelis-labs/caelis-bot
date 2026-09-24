package i18n

import "testing"

func TestResolve(t *testing.T) {
	for _, tt := range []struct {
		preference string
		system     []string
		want       Locale
	}{
		{"system", []string{"fr-FR", "zh-Hans-CN", "en"}, Chinese},
		{"system", []string{"en-US", "zh-CN"}, English},
		{"system", []string{"zh_TW"}, Chinese},
		{"system", []string{"ja"}, English},
		{"system", nil, English}, {"en", []string{"zh-CN"}, English}, {"zh-CN", []string{"en"}, Chinese},
	} {
		if got := Resolve(tt.preference, tt.system); got != tt.want {
			t.Fatalf("%+v: got %s", tt, got)
		}
	}
}
func TestNativeCatalog(t *testing.T) {
	if Text(English, "native.settings", nil) != "Settings…" || Text(Chinese, "native.settings", nil) != "设置…" {
		t.Fatal("native labels")
	}
	if Text("unsupported", "native.quit", nil) != "Quit" {
		t.Fatal("English fallback")
	}
	if Text(English, "missing.key", nil) != "missing.key" {
		t.Fatal("key fallback")
	}
	menu := Namespace(English, "native")
	menu["quit"] = "changed"
	if Text(English, "native.quit", nil) != "Quit" {
		t.Fatal("catalog mutated")
	}
}
