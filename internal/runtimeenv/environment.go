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
	"os/user"
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
		switch key {
		case "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_TURN_ID", "CODEX_APP_TOOLS_PIPE_PATH", "CODEX_INTERNAL_ORIGINATOR_OVERRIDE", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED":
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

// Report contains no exported values or paths.
type Report struct {
	HomeRestored         bool
	RecoveredPathEntries int
}
type account struct{ home, username string }

func currentAccount() account {
	if u, err := user.Current(); err == nil {
		return account{u.HomeDir, u.Username}
	}
	return account{}
}

// Resolve runs login + interactive initialization once at native app startup.
// It does not source project files or rewrite shell/config files. Both success
// and bounded failure return a usable environment without caller identity.
func Resolve(ctx context.Context, inherited []string, privateHomes ...string) ([]string, error) {
	env, _, err := ResolveWithReport(ctx, inherited, privateHomes...)
	return env, err
}

// ResolveWithReport exposes only non-sensitive restoration facts to diagnostics.
// The native application supplies its known legacy private homes; this package
// does not infer product storage from arbitrary directory names.
func ResolveWithReport(ctx context.Context, inherited []string, privateHomes ...string) ([]string, Report, error) {
	return resolve(ctx, inherited, currentAccount(), privateHomes...)
}
func resolve(ctx context.Context, inherited []string, who account, privateHomes ...string) ([]string, Report, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	report := Report{}
	v := values(Clean(inherited))
	private := func(path string) bool {
		if path == "" {
			return false
		}
		for _, known := range privateHomes {
			if known != "" && filepath.Clean(path) == filepath.Clean(known) {
				return true
			}
			if known != "" {
				a, ea := filepath.EvalSymlinks(path)
				b, eb := filepath.EvalSymlinks(known)
				if ea == nil && eb == nil && a == b {
					return true
				}
			}
		}
		return false
	}
	if !filepath.IsAbs(v["HOME"]) || private(v["HOME"]) {
		if !filepath.IsAbs(who.home) {
			return environment(v), report, errors.New("account home could not be resolved")
		}
		v["HOME"] = who.home
		report.HomeRestored = true
	}
	if private(v["ZDOTDIR"]) {
		delete(v, "ZDOTDIR")
	}
	if private(v["TMPDIR"]) {
		v["TMPDIR"] = userTemp(ctx)
	}
	for _, key := range []string{"USER", "LOGNAME"} {
		if v[key] == "" && who.username != "" {
			v[key] = who.username
		}
	}
	if v["SHELL"] == "" {
		v["SHELL"] = userShell(ctx)
	}
	if v["PATH"] == "" {
		v["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	base := environment(v)
	shell := v["SHELL"]
	if !filepath.IsAbs(shell) {
		return base, report, errors.New("user shell is not an absolute executable")
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
			return base, report, errors.New("user shell environment probe timed out or was cancelled")
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return base, report, fmt.Errorf("user shell environment probe exited with code %d", exit.ExitCode())
		}
		if errors.Is(err, os.ErrNotExist) {
			return base, report, errors.New("user shell executable was not found")
		}
		return base, report, errors.New("user shell environment could not be loaded")
	}
	start, end := []byte("\x00"+marker+"\x00"), []byte("\x00"+marker+"-end\x00")
	_, body, ok := bytes.Cut(out.Bytes(), start)
	if !ok {
		return base, report, errors.New("user shell did not emit an environment frame")
	}
	body, _, ok = bytes.Cut(body, end)
	if !ok {
		return base, report, errors.New("user shell environment frame was incomplete")
	}
	restored := values(Clean(strings.Split(string(body), "\x00")))
	if restored["PATH"] == "" {
		return base, report, errors.New("user shell returned an empty PATH")
	}
	restored["PATH"], report.RecoveredPathEntries = mergePath(restored["PATH"], v["PATH"])
	// A developer's explicit isolated profile/runtime selection must survive shell
	// startup. Working directory and shell bookkeeping belong to the host/child.
	for key, value := range v {
		if key == "HOME" || key == "TMPDIR" || key == "ZDOTDIR" || key == "USER" || key == "LOGNAME" || key == "SHELL" || key == "PWD" || key == "SHLVL" || key == "_" || key == "CODEX_HOME" || key == "CODEX_BIN" || strings.HasPrefix(key, "CAELIS_BOT_") || key == "CAELIS_CODEX_SOCKET" {
			restored[key] = value
		}
	}
	delete(restored, "OLDPWD")
	return environment(restored), report, nil
}

func environment(v map[string]string) []string {
	out := make([]string, 0, len(v))
	for key, value := range v {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}
func mergePath(shell, inherited string) (string, int) {
	seen := map[string]bool{}
	out := []string{}
	recovered := 0
	for i, path := range []string{shell, inherited} {
		for _, dir := range filepath.SplitList(path) {
			if dir == "" || seen[dir] {
				continue
			}
			seen[dir] = true
			out = append(out, dir)
			if i == 1 {
				recovered++
			}
		}
	}
	return strings.Join(out, string(os.PathListSeparator)), recovered
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
