// Package runtimeenv restores the user's exported terminal environment for a
// Finder-launched host. Runtime configuration and tools remain runtime-owned.
package runtimeenv

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const outputLimit = 1 << 20

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > outputLimit {
		return 0, errors.New("shell environment output exceeds limit")
	}
	return b.Buffer.Write(p)
}

// Clean preserves exported account/config/tool variables, excluding the launching
// agent's transient execution identity and private transports. Never log values.
func Clean(input []string) []string {
	out := make([]string, 0, len(input))
	for _, entry := range input {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" || strings.HasPrefix(key, "CAELIS_CONTROL_") {
			continue
		}
		if strings.HasPrefix(key, "CODEX_") && key != "CODEX_HOME" && key != "CODEX_API_KEY" && key != "CODEX_BIN" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func values(env []string) map[string]string {
	out := map[string]string{}
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok && key != "" {
			out[key] = value
		}
	}
	return out
}

// Resolve runs the same login + interactive initialization as a terminal, once
// at native app startup. It does not source project files or rewrite shell/config
// files. Both success and bounded failure return a usable, sanitized environment.
func Resolve(ctx context.Context, inherited []string) ([]string, error) {
	base := Clean(inherited)
	v := values(base)
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	shell := v["SHELL"]
	if shell == "" {
		shell = userShell(ctx)
	}
	if !filepath.IsAbs(shell) {
		return base, errors.New("user shell is not an absolute executable")
	}
	marker := "caelis-bot-environment-" + rand.Text()
	// Marker is generated locally from alphanumeric bytes; no user text is shell code.
	script := "printf '\\000" + marker + "\\000'; /usr/bin/env -0; printf '\\000" + marker + "-end\\000'"
	cmd := exec.CommandContext(ctx, shell, "-ilc", script)
	cmd.Dir = v["HOME"]
	cmd.Env = base
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	var out boundedOutput
	cmd.Stdout = &out
	boundProcess(cmd)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return base, errors.New("user shell environment probe timed out or was cancelled")
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return base, fmt.Errorf("user shell environment probe exited with code %d", exit.ExitCode())
		}
		if errors.Is(err, os.ErrNotExist) {
			return base, errors.New("user shell executable was not found")
		}
		return base, errors.New("user shell environment could not be loaded")
	}
	start, end := []byte("\x00"+marker+"\x00"), []byte("\x00"+marker+"-end\x00")
	_, body, ok := bytes.Cut(out.Bytes(), start)
	if !ok {
		return base, errors.New("user shell did not emit an environment frame")
	}
	body, _, ok = bytes.Cut(body, end)
	if !ok {
		return base, errors.New("user shell environment frame was incomplete")
	}
	restored := values(Clean(strings.Split(string(body), "\x00")))
	if restored["PATH"] == "" {
		return base, errors.New("user shell returned an empty PATH")
	}
	// A developer's explicit isolated profile/runtime selection must survive shell
	// startup. Working directory and shell bookkeeping belong to the host/child.
	for key, value := range v {
		if key == "HOME" || key == "USER" || key == "LOGNAME" || key == "SHELL" || key == "PWD" || key == "SHLVL" || key == "_" || key == "CODEX_HOME" || key == "CODEX_BIN" || strings.HasPrefix(key, "CAELIS_BOT_") || key == "CAELIS_CODEX_SOCKET" {
			restored[key] = value
		}
	}
	delete(restored, "OLDPWD")
	outEnv := make([]string, 0, len(restored))
	for key, value := range restored {
		outEnv = append(outEnv, key+"="+value)
	}
	sort.Strings(outEnv)
	return outEnv, nil
}

// Install is only called before the native host starts runtime/UI goroutines.
func Install(env []string) error {
	v := values(env)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, keep := v[key]; !keep {
			if err := os.Unsetenv(key); err != nil {
				return err
			}
		}
	}
	for key, value := range v {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}
