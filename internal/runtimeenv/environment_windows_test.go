package runtimeenv

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsDriveEnvironmentSurvivesStartupWithoutInvalidMutations(t *testing.T) {
	inherited := []string{`=C:=C:\工作`, `=D:=D:\Apps`, "Path=C:\\tools", "CODEX_THREAD_ID=foreign", "=", "malformed"}
	clean := Clean(inherited)
	if values(clean)["=C:"] != `C:\工作` || values(clean)["=D:"] != `D:\Apps` {
		t.Fatal("drive environment lost")
	}
	changed := map[string]string{}
	set := func(key, value string) error {
		if key == "" || strings.Contains(key, "=") {
			return fmt.Errorf("invalid key %q", key)
		}
		changed[key] = value
		return nil
	}
	unset := func(key string) error { return set(key, "<removed>") }
	if err := install(clean, inherited, set, unset); err != nil {
		t.Fatal(err)
	}
	if changed["CODEX_THREAD_ID"] != "<removed>" || changed["PATH"] != `C:\tools` {
		t.Fatal("normal environment cleanup changed", changed)
	}
}

func TestWindowsAccountEnvironmentDoesNotRunShell(t *testing.T) {
	home := t.TempDir()
	private := filepath.Join(home, "old-bot-home")
	env, report, err := resolvePlatform(t.Context(), []string{"Path=C:\\tools;C:\\Windows\\System32", "ComSpec=C:\\Windows\\System32\\cmd.exe", "HOME=" + private, "UserProfile=" + home, "codex_thread_id=synthetic", "Caelis_Control_token=synthetic", "CODEX_HOME=" + filepath.Join(home, ".codex"), "SHELL=C:\\must-not-run.exe"}, account{home: home}, private)
	if err != nil || !report.HomeRestored {
		t.Fatal("environment recovery", err)
	}
	v := values(env)
	if v["HOME"] != home || v["USERPROFILE"] != home || v["PATH"] == "" || v["COMSPEC"] == "" || v["CODEX_HOME"] == "" {
		t.Fatal("account/tool environment lost")
	}
	if _, ok := v["CODEX_THREAD_ID"]; ok {
		t.Fatal("caller identity leaked")
	}
	if _, ok := v["CAELIS_CONTROL_TOKEN"]; ok {
		t.Fatal("caller transport leaked")
	}
}
