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
