package caelis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

type executionFixture struct {
	env               []string
	home, dotdir, bin string
}

func executionQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func newExecutionFixture(t *testing.T, root string) executionFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	f := executionFixture{home: filepath.Join(root, "home"), dotdir: filepath.Join(root, "shell-config"), bin: filepath.Join(root, "user-tools")}
	for _, dir := range []string{f.home, f.dotdir, f.bin, filepath.Join(root, "tmp")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		filepath.Join(f.home, "cli-config"):    "cli-ok",
		filepath.Join(f.bin, "fixture-cli"):    "#!/bin/sh\n/bin/cat \"$HOME/cli-config\"\n",
		filepath.Join(f.dotdir, ".zprofile"):   "export PATH=" + executionQuote(f.bin) + ":$PATH\nprintf x >> " + executionQuote(filepath.Join(f.home, "probe-count")) + "\n",
		filepath.Join(f.dotdir, ".zshrc"):      "export TOOL_SETTING=from-shell\n",
		filepath.Join(f.home, ".bash_profile"): "export PATH=/usr/bin:/bin\nexport INITIALIZED_TWICE=yes\n",
	} {
		if err := os.WriteFile(name, []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	f.env, err = runtimeenv.Resolve(t.Context(), []string{"HOME=" + f.home, "ZDOTDIR=" + f.dotdir, "SHELL=/bin/zsh", "USER=fixture-user", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "TMPDIR=" + filepath.Join(root, "tmp"), "HOST_ONLY=present", "ENV_EPOCH=initial"})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f executionFixture) check(t *testing.T, cwd string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(cwd)
	if err == nil {
		cwd = resolved
	} else {
		parent, e := filepath.EvalSymlinks(filepath.Dir(cwd))
		if e != nil {
			t.Fatal(e)
		}
		cwd = filepath.Join(parent, filepath.Base(cwd))
	}
	return `test "$HOME" = ` + executionQuote(f.home) + ` && test "$PWD" = ` + executionQuote(cwd) + ` && test "$ZDOTDIR" = ` + executionQuote(f.dotdir) + ` && test "$TOOL_SETTING" = from-shell && test "$(fixture-cli)" = cli-ok && test -z "${INITIALIZED_TWICE+x}"`
}
func requireExecutionFile(t *testing.T, dir, name, want string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || string(b) != want {
		t.Fatalf("execution fixture %s did not produce expected result: %v", name, err)
	}
}
func verifyExecutionConfiguration(t *testing.T, ctx context.Context, s *Session, host *client, model *acceptanceModel, root, cwd string, f executionFixture) {
	t.Helper()
	config, err := s.Configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Profile.ExecutionConfig
	if c == nil || c.Environment == nil || !value(c.Environment.Inherit) || c.Shell != nil && value(c.Shell.Login) || len(c.Environment.Set) != 0 {
		t.Fatal("public profile lost execution policy")
	}
	model.set("CASE_ENV_SYNC", modelStep{Name: "RunCommand", Args: map[string]any{"command": f.check(t, cwd) + ` && test "$HOST_ONLY" = present && printf ok > env-sync.txt`}})
	submitAcceptance(t, ctx, s, "CASE_ENV_SYNC")
	requireExecutionFile(t, cwd, "env-sync.txt", "ok")
	// The same environment must reach a TTY process and survive later input.
	model.set("CASE_ENV_TTY", modelStep{Name: "RunCommand", Args: map[string]any{"command": `read -r input; ` + f.check(t, cwd) + ` && printf '%s' "$input" > env-tty.txt`, "tty": true, "yield_time_ms": 0}}, modelStep{Reply: "TTY_READY"})
	submitAcceptance(t, ctx, s, "CASE_ENV_TTY")
	var jobs wire.TaskList
	if err = s.client.json(ctx, "GET", "/sessions/"+idPath(s.state.Session.SessionId)+"/tasks", nil, &jobs, "", ""); err != nil {
		t.Fatal(err)
	}
	handle := ""
	for _, job := range jobs.Tasks {
		if value(job.SupportsInput) && job.Running {
			handle = job.Handle
		}
	}
	if handle == "" {
		t.Fatal("native TTY handle missing")
	}
	model.set("CASE_ENV_INPUT", modelStep{Name: "Task", Args: map[string]any{"action": "write", "handle": handle, "input": "input-ok"}}, modelStep{Name: "Task", Args: map[string]string{"action": "wait", "handle": handle}})
	submitAcceptance(t, ctx, s, "CASE_ENV_INPUT")
	requireExecutionFile(t, cwd, "env-tty.txt", "input-ok")
	// Environment inheritance must not widen sandbox writes.
	outside := filepath.Join(f.home, "outside-workspace.txt")
	model.set("CASE_ENV_SANDBOX", modelStep{Name: "RunCommand", Args: map[string]any{"command": "if (printf forbidden > " + executionQuote(outside) + ") 2>/dev/null; then exit 9; else printf denied > env-sandbox.txt; fi"}})
	submitAcceptance(t, ctx, s, "CASE_ENV_SANDBOX")
	requireExecutionFile(t, cwd, "env-sandbox.txt", "denied")
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("environment inheritance widened sandbox writes")
	}
	// Explicit empty inheritance and inherited overrides use separate Sessions.
	var created, result wire.CommandResult
	var sid, op string
	for _, tc := range []struct {
		name        string
		environment *wire.EnvironmentConfig
	}{
		{"empty", &wire.EnvironmentConfig{Inherit: pointer(false), Set: map[string]any{"HOME": f.home, "PATH": f.bin + ":/usr/bin:/bin", "ZDOTDIR": f.dotdir, "TOOL_SETTING": "from-shell", "SESSION_VALUE": "configured", "EMPTY_VALUE": ""}}},
		{"overrides", &wire.EnvironmentConfig{Inherit: pointer(true), Set: map[string]any{"SESSION_VALUE": "configured", "EMPTY_VALUE": ""}, Unset: []string{"HOST_ONLY"}}},
	} {
		other := filepath.Join(root, tc.name+"-session")
		if err = os.Mkdir(other, 0700); err != nil {
			t.Fatal(err)
		}
		profile := config.Profile
		profile.Workspace = &wire.ApplicationWorkspace{Cwd: &other}
		profile.ExecutionConfig = &wire.ExecutionConfig{Environment: tc.environment, Shell: &wire.ShellConfig{Login: pointer(false)}}
		op = "environment-session-" + tc.name
		created = wire.CommandResult{}
		if err = s.client.json(ctx, "POST", "/application/sessions", wire.CreateApplicationSessionRequest{OperationId: &op, Profile: profile}, &created, op, ""); err != nil || !succeeded(created.Outcome) {
			t.Fatal("configured session creation failed", err)
		}
		sid = value(created.SessionId)
		if sid == "" && created.Resource != nil {
			sid = value(created.Resource.Ref)
		}
		key := "CASE_ENV_" + strings.ToUpper(tc.name)
		model.set(key, modelStep{Name: "RunCommand", Args: map[string]any{"command": f.check(t, other) + ` && test -z "${HOST_ONLY+x}" && test "$SESSION_VALUE" = configured && test "${EMPTY_VALUE+x}" = x && printf ok > configured.txt`}})
		op = "environment-prompt-" + tc.name
		if err = s.client.json(ctx, "POST", "/application/sessions/"+idPath(sid)+"/prompt", wire.ApplicationPromptRequest{OperationId: &op, Input: &key, SourceKind: "user"}, &result, op, ""); err != nil || !succeeded(result.Outcome) {
			t.Fatal("configured prompt rejected", err)
		}
		waitAcceptance(t, ctx, func() bool { _, err := os.Stat(filepath.Join(other, "configured.txt")); return err == nil })
		requireExecutionFile(t, other, "configured.txt", "ok")
	}
	if _, err = s.UpdateConfiguration(ctx, "environment-is-creation-bound", string(config.Revision), map[string]any{"execution_config": map[string]any{}}); err == nil {
		t.Fatal("execution configuration accepted as a hot update")
	}
	// A normal Session on that Host still inherits the original user environment.
	ordinary := filepath.Join(root, "ordinary-session")
	if err = os.Mkdir(ordinary, 0700); err != nil {
		t.Fatal(err)
	}
	op = "ordinary-environment-session"
	if err = host.json(ctx, "POST", "/sessions", wire.CreateSessionRequest{OperationId: &op, Cwd: &ordinary}, &created, op, ""); err != nil || !succeeded(created.Outcome) {
		t.Fatal("ordinary session creation failed", err)
	}
	sid = value(created.SessionId)
	if sid == "" && created.Resource != nil {
		sid = value(created.Resource.Ref)
	}
	model.set("CASE_ENV_ORDINARY", modelStep{Name: "RunCommand", Args: map[string]any{"command": f.check(t, ordinary) + ` && test "$HOST_ONLY" = present && test -z "${SESSION_VALUE+x}" && printf ok > ordinary.txt`}})
	op = "ordinary-environment-prompt"
	if err = host.json(ctx, "POST", "/sessions/"+idPath(sid)+"/prompt", wire.PromptRequest{OperationId: &op, Input: pointer("CASE_ENV_ORDINARY")}, &result, op, ""); err != nil || !succeeded(result.Outcome) {
		t.Fatal("ordinary prompt rejected", err)
	}
	waitAcceptance(t, ctx, func() bool { _, err := os.Stat(filepath.Join(ordinary, "ordinary.txt")); return err == nil })
	requireExecutionFile(t, ordinary, "ordinary.txt", "ok")
	requireExecutionFile(t, f.home, "probe-count", "x")
}
