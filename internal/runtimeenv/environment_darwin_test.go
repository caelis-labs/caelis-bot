package runtimeenv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinderEnvironmentLoadsLoginAndInteractiveTools(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "npx"), []byte("#!/bin/sh\nprintf '%s' \"$TOOL_SETTING\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	if err := os.WriteFile(filepath.Join(dir, ".zprofile"), []byte("export PATH="+quote(bin)+":$PATH\necho login-banner\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".zshrc"), []byte("export TOOL_SETTING='native-worker-tools'\nexport CODEX_HOME='/incorrect-shell-override'\nexport CODEX_THREAD_ID='foreign'\nexport CAELIS_CONTROL_TOKEN='foreign'\necho interactive-banner\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input := []string{"HOME=" + dir, "ZDOTDIR=" + dir, "SHELL=/bin/zsh", "PATH=/usr/bin:/bin", "CODEX_HOME=/explicit-profile", "CAELIS_BOT_DATA_DIR=/isolated-bot", "CODEX_APP_TOOLS_PIPE_PATH=/foreign", "PWD=/original"}
	env, err := Resolve(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	v := values(env)
	if v["CODEX_HOME"] != "/explicit-profile" || v["CAELIS_BOT_DATA_DIR"] != "/isolated-bot" || v["PWD"] != "/original" || v["CODEX_THREAD_ID"] != "" || v["CODEX_APP_TOOLS_PIPE_PATH"] != "" || v["CAELIS_CONTROL_TOKEN"] != "" {
		t.Fatal("runtime/profile boundary changed")
	}
	cmd := exec.Command("/usr/bin/env", "npx")
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil || string(output) != "native-worker-tools" {
		t.Fatalf("runtime child could not use terminal tool: %v", err)
	}
}

func TestShellFailureIsBoundedAndRetainsInheritedConfiguration(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	input := []string{"HOME=" + t.TempDir(), "SHELL=/bin/zsh", "PATH=/fixture/tools:/usr/bin:/bin", "TOOL_SETTING=retained", "CODEX_THREAD_ID=foreign"}
	env, err := Resolve(ctx, input)
	if err == nil || values(env)["TOOL_SETTING"] != "retained" || values(env)["CODEX_THREAD_ID"] != "" {
		t.Fatal("failed startup lost configuration or identity isolation")
	}
	input[1] = "SHELL=relative-shell"
	if _, err := Resolve(t.Context(), input); err == nil {
		t.Fatal("relative shell accepted")
	}
}

func TestLegacyNotebookEnvironmentRestoresAccountAndSeparatesCWD(t *testing.T) {
	home, notebook := t.TempDir(), t.TempDir()
	bin := filepath.Join(home, "custom-tools")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		filepath.Join(home, ".zprofile"):  `export PATH="$HOME/custom-tools:$PATH"` + "\n",
		filepath.Join(home, ".zshrc"):     "export TOOL_SETTING=from-real-account\n",
		filepath.Join(notebook, ".zshrc"): "export TOOL_SETTING=wrong-private-home\n",
		filepath.Join(bin, "fixture-cli"): "#!/bin/sh\nprintf '%s' \"$TOOL_SETTING\"\n",
	} {
		if err := os.WriteFile(name, []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	env, report, err := resolve(t.Context(), []string{"HOME=" + notebook, "ZDOTDIR=" + notebook, "TMPDIR=" + notebook, "SHELL=/bin/zsh", "PATH=/usr/bin:/bin"}, account{home, "fixture-user"}, notebook)
	if err != nil {
		t.Fatal(err)
	}
	v := values(env)
	if !report.HomeRestored || v["HOME"] != home || v["ZDOTDIR"] == notebook || v["TMPDIR"] == notebook || !filepath.IsAbs(v["TMPDIR"]) || v["USER"] != "fixture-user" || v["LOGNAME"] != "fixture-user" {
		t.Fatal("account environment was not restored")
	}
	cmd := exec.Command("/usr/bin/env", "fixture-cli")
	cmd.Env, cmd.Dir = env, notebook
	out, err := cmd.Output()
	if err != nil || string(out) != "from-real-account" {
		t.Fatalf("runtime child lost account toolchain: %v", err)
	}
}

func TestCustomShellConfigurationAndInheritedToolsSurviveProbe(t *testing.T) {
	home, dotdir, inheritedBin := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dotdir, ".zshrc"), []byte("export PATH=/usr/bin:/bin\nexport TOOL_SETTING=custom-dotdir\nexport CODEX_CUSTOM_CONFIG=retained\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inheritedBin, "fixture-cli"), []byte("#!/bin/sh\nprintf '%s' \"$TOOL_SETTING\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	env, report, err := resolve(t.Context(), []string{"HOME=" + home, "ZDOTDIR=" + dotdir, "SHELL=/bin/zsh", "PATH=" + inheritedBin + ":/usr/bin:/bin:" + inheritedBin}, account{t.TempDir(), "fixture-user"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v := values(env)
	if report.HomeRestored || report.RecoveredPathEntries != 1 || v["HOME"] != home || v["ZDOTDIR"] != dotdir || v["PATH"] != "/usr/bin:/bin:"+inheritedBin || v["CODEX_CUSTOM_CONFIG"] != "retained" {
		t.Fatal("custom configuration or inherited PATH changed")
	}
	cmd := exec.Command("/usr/bin/env", "fixture-cli")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil || string(out) != "custom-dotdir" {
		t.Fatalf("inherited tool was lost after shell PATH replacement: %v", err)
	}
}

func TestProbeFailureStillRepairsKnownPrivateHome(t *testing.T) {
	home, notebook := t.TempDir(), t.TempDir()
	env, report, err := resolve(t.Context(), []string{"HOME=" + notebook, "ZDOTDIR=" + notebook, "SHELL=/missing-shell", "PATH=/fixture/bin:/usr/bin:/bin", "TOOL_SETTING=retained"}, account{home, "fixture-user"}, notebook)
	v := values(env)
	if err == nil || !report.HomeRestored || v["HOME"] != home || v["ZDOTDIR"] != "" || v["PATH"] != "/fixture/bin:/usr/bin:/bin" || v["TOOL_SETTING"] != "retained" {
		t.Fatal("failed shell probe lost corrected inherited environment")
	}
	for _, badHome := range []string{"", "relative-home"} {
		env, _, _ := resolve(t.Context(), []string{"HOME=" + badHome, "SHELL=/missing-shell"}, account{home, "fixture-user"})
		if values(env)["HOME"] != home {
			t.Fatal("missing or relative HOME was not repaired")
		}
	}
}

func TestCleanPreservesConfigurationOnlyRemovingCallerContext(t *testing.T) {
	v := values(Clean([]string{"CODEX_CUSTOM_CONFIG=keep", "CODEX_API_KEY=fixture", "CODEX_HOME=/profile", "SOME_TOOL_SETTING=keep", "CODEX_THREAD_ID=remove", "CODEX_SANDBOX=remove", "CODEX_APP_TOOLS_PIPE_PATH=remove", "CAELIS_CONTROL_TOKEN=remove"}))
	for _, key := range []string{"CODEX_CUSTOM_CONFIG", "CODEX_API_KEY", "CODEX_HOME", "SOME_TOOL_SETTING"} {
		if v[key] == "" {
			t.Fatalf("configuration %s removed", key)
		}
	}
	for _, key := range []string{"CODEX_THREAD_ID", "CODEX_SANDBOX", "CODEX_APP_TOOLS_PIPE_PATH", "CAELIS_CONTROL_TOKEN"} {
		if _, exists := v[key]; exists {
			t.Fatalf("caller context %s inherited", key)
		}
	}
}
