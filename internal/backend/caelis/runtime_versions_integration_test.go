package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

// This opt-in test executes the Bot adapter from its own synthetic app bundle,
// with an actual released CLI helper and a different global/Host CLI version.
func TestBundledRuntimeVersionInterop(t *testing.T) {
	old, current := os.Getenv("CAELIS_BOT_TEST_PREVIOUS_BINARY"), os.Getenv("CAELIS_BOT_TEST_BINARY")
	if old == "" || current == "" {
		t.Skip("requires two explicitly selected Caelis binaries")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("macOS embedded-runtime acceptance")
	}
	budget := 120 * time.Second
	if os.Getenv("BOT_RUNTIME_MATRIX_CHILD") == "" {
		budget = 150 * time.Second
	}
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	if os.Getenv("BOT_RUNTIME_MATRIX_CHILD") == "" {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			_ = exec.CommandContext(cleanup, current, "service", "stop", "--store-dir", filepath.Join(root, "store"), "--format", "json").Run()
		}()
		contents := filepath.Join(root, "Matrix.app", "Contents")
		for _, dir := range []string{filepath.Join(contents, "MacOS"), filepath.Join(contents, "Helpers"), filepath.Join(root, "home"), filepath.Join(root, "global")} {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := filepath.Join(contents, "MacOS", "matrix-test")
		for dst, src := range map[string]string{child: executable, filepath.Join(contents, "Helpers", "caelis"): current, filepath.Join(root, "global", "caelis"): old} {
			data, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, data, 0700); err != nil {
				t.Fatal(err)
			}
		}
		command := exec.CommandContext(ctx, child, "-test.run=^TestBundledRuntimeVersionInterop$", "-test.v")
		command.Dir = root
		command.Env = []string{"BOT_RUNTIME_MATRIX_CHILD=1", "CAELIS_BOT_TEST_PREVIOUS_BINARY=" + old, "CAELIS_BOT_TEST_BINARY=" + current, "HOME=" + filepath.Join(root, "home"), "XDG_CONFIG_HOME=" + filepath.Join(root, "home"), "PATH=" + filepath.Join(root, "global") + ":/usr/bin:/bin:/usr/sbin:/sbin", "TMPDIR=" + os.TempDir()}
		output, err := command.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	settings := api.RuntimeSettings{Runtime: "caelis", CaelisStore: filepath.Join(root, "store")}
	cli := func(binary string, args ...string) []byte {
		t.Helper()
		args = append(args, "--store-dir", settings.CaelisStore, "--format", "json")
		output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("isolated CLI failed: %v %s", err, output)
		}
		return output
	}
	cli(old, "service", "start")
	defer func() {
		_ = exec.Command(current, "service", "stop", "--store-dir", settings.CaelisStore, "--format", "json").Run()
	}()
	discovery, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	host, err := newClient(discovery.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	model := newAcceptanceModel()
	server := httptest.NewServer(http.HandlerFunc(model.serve))
	defer server.Close()
	var status wire.StatusSnapshot
	if err := host.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
		t.Fatal(err)
	}
	op := "matrix-configure"
	var result wire.CommandResult
	err = host.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: "custom-responses", Model: "matrix", BaseUrl: pointer(server.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &result, op, string(status.Configuration.Revision))
	if err != nil || !succeeded(result.Outcome) {
		t.Fatalf("configure: %+v %v", result, err)
	}
	selector := fixtureModelSelector(t, ctx, host, "matrix")
	held, entered := make(chan struct{}), make(chan struct{}, 1)
	model.set("CASE_OLD_BLOCK", modelStep{Block: held, Entered: entered, Reply: "OLD_COMPLETED"})
	oldTask := exec.CommandContext(ctx, old, "-p", "CASE_OLD_BLOCK", "--store-dir", settings.CaelisStore, "--format", "json")
	if err := oldTask.Start(); err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			close(held)
		}
		if oldTask.ProcessState == nil {
			_ = oldTask.Wait()
		}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("old Session did not run")
	}
	found, err := caelisruntime.Find("")
	if err != nil || !strings.Contains(found, "Contents/Helpers/caelis") {
		t.Fatalf("bundle selection: %s %v", found, err)
	}
	bot := New(Options{RequireApproval: true, Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: selector, ApprovalMode: "workspace-write"}})
	defer bot.Close(context.Background())
	var effects atomic.Int32
	tools := &acceptanceTools{defs: fixtureDefinitions("string"), call: func(context.Context, string, json.RawMessage) api.ToolResult {
		effects.Add(1)
		err := os.WriteFile(filepath.Join(root, "effect.txt"), []byte("BUNDLED_CALLBACK_EFFECT"), 0600)
		return api.ToolResult{IsError: err != nil, Content: []map[string]string{{"type": "text", "text": "CALLBACK_COMPLETE"}}}
	}}
	if err := bot.ConfigureBotTools(&api.ToolConnection{ApprovedTools: []string{"FixtureLookup"}, Host: tools, NotebookDirectory: root, Instructions: "Version interoperation acceptance"}); err != nil {
		t.Fatal(err)
	}
	if err := bot.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	waitAcceptance(t, ctx, func() bool { return bot.Snapshot().CanSend })
	bot.mu.Lock()
	sid, storeID, principal := bot.state.Session.SessionId, bot.state.StoreID, bot.state.PrincipalID
	bot.mu.Unlock()
	model.set("CASE_BUNDLE_BEFORE", modelStep{Name: "FixtureLookup", Args: map[string]string{"key": "before"}}, modelStep{Reply: "BOT_BEFORE_COMPLETE"})
	submitAcceptance(t, ctx, bot, "CASE_BUNDLE_BEFORE")
	still, _, err := Discover(settings)
	if err != nil || still.InstanceID != discovery.InstanceID {
		t.Fatal("running Session was interrupted", err)
	}
	close(held)
	released = true
	if err := oldTask.Wait(); err != nil {
		t.Fatal("original Session failed", err)
	}
	waitAcceptance(t, ctx, func() bool { d, _, e := Discover(settings); return e == nil && d.InstanceID != discovery.InstanceID })
	waitAcceptance(t, ctx, func() bool {
		bot.mu.Lock()
		defer bot.mu.Unlock()
		return bot.connected && bot.info.InstanceId != nil && *bot.info.InstanceId != discovery.InstanceID
	})
	if err := bot.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	bot.mu.Lock()
	same := sid == bot.state.Session.SessionId && storeID == bot.state.StoreID && principal == bot.state.PrincipalID
	bot.mu.Unlock()
	if !same || effects.Load() != 1 {
		t.Fatal("reconnect changed identity or replayed callback")
	}
	model.set("CASE_BUNDLE_AFTER", modelStep{Name: "FixtureLookup", Args: map[string]string{"key": "after"}}, modelStep{Reply: "BOT_AFTER_COMPLETE"})
	submitAcceptance(t, ctx, bot, "CASE_BUNDLE_AFTER")
	if effects.Load() != 2 {
		t.Fatal("callback count", effects.Load())
	}
	if b, err := os.ReadFile(filepath.Join(root, "effect.txt")); err != nil || string(b) != "BUNDLED_CALLBACK_EFFECT" {
		t.Fatal("callback file effect missing", err)
	}
	model.set("CASE_GLOBAL_AFTER", modelStep{Reply: "GLOBAL_AFTER_COMPLETE"})
	if output := cli(old, "-p", "CASE_GLOBAL_AFTER"); !strings.Contains(string(output), "GLOBAL_AFTER_COMPLETE") {
		t.Fatal("older CLI did not complete against new Host")
	}
	t.Log("PASS: bundled CLI selected over global; old active Session preserved; automatic Host upgrade; original Bot identity and callback receipts preserved; old CLI completes against new Host")
}
