package runtimeenv

import (
	"path/filepath"
	"testing"
)

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
