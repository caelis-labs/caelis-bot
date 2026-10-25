package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDevelopmentDefaultPreservesExplicitAndReleaseProfiles(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, "Caelis Bot Dev")
	os.MkdirAll(p, 0700)
	old := buildChannel
	t.Cleanup(func() { buildChannel = old })
	buildChannel = "development"
	os.WriteFile(filepath.Join(p, "active-profile.json"), []byte(`{"dataDirectory":"relative"}`), 0600)
	if _, e := developmentDefault(base); e == nil {
		t.Fatal("invalid profile falls back into old data")
	}
	target := filepath.Join(base, "Daily")
	home := filepath.Join(base, "Daily Codex")
	os.MkdirAll(target, 0700)
	os.MkdirAll(home, 0700)
	b := `{"dataDirectory":` + quotedProfilePath(target) + `,"codexHome":` + quotedProfilePath(home) + `}`
	os.WriteFile(filepath.Join(p, "active-profile.json"), []byte(b), 0600)
	v, e := developmentDefault(base)
	if e != nil || v.DataDirectory != target || v.CodexHome != home {
		t.Fatal(v, e)
	}
	buildChannel = "release"
	if v, e := developmentDefault(base); e != nil || v.DataDirectory != "" {
		t.Fatal("release adopted dev data", v, e)
	}
	buildChannel = "development"
	t.Setenv("CAELIS_BOT_DATA_DIR", filepath.Join(base, "explicit"))
	if got, e := applicationDataDirectory(); e != nil || got != filepath.Join(base, "explicit") {
		t.Fatal("explicit test root lost", got, e)
	}
}

func quotedProfilePath(path string) string { b, _ := json.Marshal(path); return string(b) }

func TestDevelopmentProfileBasePrecedesHOMERecovery(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS account-owned selector")
	}
	want, err := developmentProfileBase()
	if err != nil || !filepath.IsAbs(want) {
		t.Fatal(want, err)
	}
	t.Setenv("HOME", "")
	os.Unsetenv("HOME")
	if got, err := developmentProfileBase(); err != nil || got != want {
		t.Fatal("missing launcher HOME prevented account selection", got, err)
	}
	t.Setenv("HOME", t.TempDir())
	if got, err := developmentProfileBase(); err != nil || got != want {
		t.Fatal("inherited workspace HOME changed account selection", got, err)
	}
}
func TestDevelopmentPrivateCodexHomeNeverFallsBackOnEmptyInheritedValue(t *testing.T) {
	v := developmentProfile{DataDirectory: filepath.Join(t.TempDir(), "data"), CodexHome: filepath.Join(t.TempDir(), "private-codex")}
	for _, tc := range []struct {
		name, value, want string
		unset             bool
	}{{"unset", "", v.CodexHome, true}, {"empty", "", v.CodexHome, false}, {"explicit", filepath.Join(t.TempDir(), "explicit"), "", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CAELIS_BOT_DATA_DIR", "temporary")
			os.Unsetenv("CAELIS_BOT_DATA_DIR")
			t.Setenv("CODEX_HOME", tc.value)
			if tc.unset {
				os.Unsetenv("CODEX_HOME")
			}
			if e := applyDevelopmentProfileEnvironment(v); e != nil {
				t.Fatal(e)
			}
			want := tc.want
			if tc.name == "explicit" {
				want = tc.value
			}
			if os.Getenv("CODEX_HOME") != want || os.Getenv("CAELIS_BOT_DATA_DIR") != v.DataDirectory {
				t.Fatal("private Runtime home not preserved")
			}
		})
	}
}
